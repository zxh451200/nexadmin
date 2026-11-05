package file

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func zipTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

// 三个同名文件必须落成三个不同条目：旧写法只在原始名上计数，第二个和第三个同名都算成 a-2.txt，
// 而 zip.Writer 不拒绝重名——后写的顶掉前一个，用户少拿一个文件且毫不知情。
func TestZipEntryNamesAreUnique(t *testing.T) {
	used := map[string]bool{}
	var got []string
	for i := 0; i < 3; i++ {
		got = append(got, zipEntryName(used, "a.txt"))
	}
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Fatalf("三个同名文件拿到了重名条目 %v", got)
		}
		seen[n] = true
	}
	if got[0] != "a.txt" {
		t.Errorf("第一个同名应当保留原名，实际 %q", got[0])
	}
}

// 历史行里的名字可能带分隔符：领用时必须清成单层文件名，不能让 ../x 变成能跳出目录的条目。
func TestZipEntryNameSanitizesPathishNames(t *testing.T) {
	for _, raw := range []string{"../../etc/passwd", `a\b.txt`, "../x.txt", ".", ".."} {
		name := zipEntryName(map[string]bool{}, raw)
		if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || name == "" {
			t.Errorf("%q 清成了可跳出目录或空的名字 %q", raw, name)
		}
	}
}

// 读不出内容/外链不能被静默吞掉：随包交回 _FAILED.txt 点名。响应已经在流式写出，
// 状态码和响应体都改不了，用户能看到的只有解压出来的东西。
func TestWriteFilesZipReportsSkipped(t *testing.T) {
	mem := &memStore{data: map[string][]byte{"keep.txt": []byte("内容")}}
	h := &Handler{svc: &Service{store: &store{blobs: mem}}}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := []File{
		{Name: "keep.txt", StoragePath: "keep.txt"},
		{Name: "gone.txt", StoragePath: "gone.txt"}, // 对象丢失 → 读失败
		{Name: "link.txt", StoragePath: ""},         // 外链：没有字节可打包
	}
	ctx := i18n.WithLang(context.Background(), "zh-CN")
	if err := h.writeFilesZip(ctx, zw, files); err != nil {
		t.Fatalf("写包失败：%v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	manifest := ""
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == zipManifestName {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			manifest = string(b)
		}
	}
	if len(names) != 2 || names[0] != "keep.txt" {
		t.Fatalf("包内应是「1 个能读出的文件 + 1 份清单」，实际 %v", names)
	}
	// 清单必须点出被跳过的两个名字，否则用户不知道少拿了文件
	for _, want := range []string{"gone.txt", "link.txt"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("清单没点名被跳过的 %s：%q", want, manifest)
		}
	}
	if !strings.HasPrefix(manifest, "\uFEFF") {
		t.Errorf("清单缺 BOM，Windows 记事本打开会按本地代码页显示成乱码")
	}
	if lines := strings.Count(strings.TrimRight(manifest, "\r\n"), "\r\n"); lines < 3 {
		t.Errorf("清单行数不对（应有表头 + 2 条）：%q", manifest)
	}
}

// 读失败的文件不该占掉一个序号：a.txt 读失败时，下一个 a.txt 仍应拿到 a.txt 而不是 a-2.txt。
func TestWriteFilesZipDoesNotBurnSequenceOnSkip(t *testing.T) {
	mem := &memStore{data: map[string][]byte{"k2": []byte("内容")}}
	h := &Handler{svc: &Service{store: &store{blobs: mem}}}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := []File{
		{Name: "a.txt", StoragePath: "missing"},
		{Name: "a.txt", StoragePath: "k2"},
	}
	if err := h.writeFilesZip(context.Background(), zw, files); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["a.txt"] {
		t.Errorf("读失败的第一条占掉了序号，第二条被存成 a-2.txt：%v", keysOf(found))
	}
}

func keysOf(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// 上限必须在写第一个字节之前就给出：一旦开始流式输出，状态码和错误体都改不了，
// 「200 + 半截包」比「直接拒绝」更坏。数量判定还必须先于 SQL——
// 一条 800 个占位符的 IN (...) 本身就是被打死的对象。
func TestFilesForDownloadRejectsOverLimitWithoutQuery(t *testing.T) {
	db, mock := zipTestDB(t)
	s := &Service{db: db}

	ids := make([]idgen.ID, MaxZipFiles+1)
	for i := range ids {
		ids[i] = idgen.ID(i + 1)
	}
	_, err := s.FilesForDownload(context.Background(), ids)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || re.Key != "file.zip_too_many" {
		t.Fatalf("超过数量上限应回 400 file.zip_too_many，实际 %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("超限判定不该查库：%v", err)
	}

	if _, err := s.FilesForDownload(context.Background(), nil); err == nil {
		t.Fatal("空选择应当拒绝")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("空选择不该查库：%v", err)
	}
}

// 总大小按台账 size 累加，超限在打包前拒绝，且错误参数要带上算出来的 MB 数。
func TestFilesForDownloadRejectsOversizeTotal(t *testing.T) {
	db, mock := zipTestDB(t)
	s := &Service{db: db}
	mock.ExpectQuery("SELECT .+FROM `sys_file`").WillReturnRows(sqlmock.NewRows(
		[]string{"id", "name", "storage_path", "size"}).
		AddRow(int64(1), "big.zip", "k/big.zip", MaxZipBytes+1).
		AddRow(int64(2), "small.txt", "k/small.txt", int64(10)))

	_, err := s.FilesForDownload(context.Background(), []idgen.ID{1, 2})
	var re *response.Error
	if !errors.As(err, &re) || re.Key != "file.zip_too_large" {
		t.Fatalf("超过总大小上限应回 file.zip_too_large，实际 %v", err)
	}
	if len(re.Args) != 2 {
		t.Fatalf("超限文案要带「实际多少 / 上限多少」两个数字，实际参数 %v", re.Args)
	}
	if got, ok := re.Args[0].(float64); !ok || got < 1024 {
		t.Errorf("实际大小应当是 %g MB 量级，参数 %v", float64(MaxZipBytes+1)/(1<<20), re.Args)
	}
}

// 点名了但一个都拿不到（跨租户、已删除、全是外链）必须明确拒绝：
// 回一个空 zip 等于「什么都没发生」，用户只会以为下载坏了。
func TestFilesForDownloadRejectsNothingPackable(t *testing.T) {
	db, mock := zipTestDB(t)
	s := &Service{db: db}
	mock.ExpectQuery("SELECT .+FROM `sys_file`").WillReturnRows(sqlmock.NewRows(
		[]string{"id", "name", "storage_path", "size"}).
		AddRow(int64(1), "link.txt", "", int64(0)))
	_, err := s.FilesForDownload(context.Background(), []idgen.ID{1})
	var re *response.Error
	if !errors.As(err, &re) || re.Key != "file.zip_empty" {
		t.Fatalf("全外链应回 file.zip_empty，实际 %v", err)
	}
}

// 包内条目名与清单名不能互相顶掉：清单名预留了，真叫 _FAILED.txt 的文件要让行。
func TestZipManifestNameIsReserved(t *testing.T) {
	used := map[string]bool{zipManifestName: true}
	if got := zipEntryName(used, "_FAILED.txt"); got == zipManifestName {
		t.Errorf("与清单同名的文件应当让行，实际拿到 %q", got)
	}
}
