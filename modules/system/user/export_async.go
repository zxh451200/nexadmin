package user

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// 异步导出：导出一律登记 core/job 任务，worker 流式写 xlsx 到存储，
// 用户在“导出中心”下载（无论数据量大小，行为一致；小任务几秒完成）。
const (
	jobTypeUserExport = "user.export"
	asyncExportMax    = 500000 // 单次异步导出行数上限（xlsx 单表上限约 104 万行，留足余量）
	exportBatch       = 1000   // 每批读取行数
)

// exportParams 是任务参数：只含筛选条件和操作者身份，不含任何凭据。
// 数据范围在执行时按任务所有者重新计算（worker 里没有 request 上下文）：角色、部门、超管身份都以执行时的数据库为准，
// 所以角色变更、降级后再重试也不会越权；Super 字段只为兼容旧任务保留，执行时不再采信。
type exportParams struct {
	Keyword     string   `json:"keyword,omitempty"`
	Status      string   `json:"status,omitempty"`
	DeptID      string   `json:"deptId,omitempty"`
	Gender      string   `json:"gender,omitempty"`
	CreatedFrom string   `json:"createdFrom,omitempty"`
	CreatedTo   string   `json:"createdTo,omitempty"`
	ActorID     idgen.ID `json:"actorId"`
	Super       bool     `json:"super,omitempty"` // 已弃用：提交时的快照，执行时忽略
}

func (p exportParams) query() listQuery {
	// Status/DeptID/Gender 是逗号拼接的多值筛选串（与 query 参数同一契约）；旧在途任务的单值天然兼容。
	return listQuery{
		Keyword: p.Keyword,
		Status:  response.ParseList(p.Status, response.MaxFilterValues),
		DeptIDs: response.ParseFilterIDs(p.DeptID, response.MaxFilterValues),
		Gender:  response.ParseList(p.Gender, response.MaxFilterValues),
		CreatedFrom: p.CreatedFrom, CreatedTo: p.CreatedTo,
	}
}

// exportJob 是 user.export 的任务处理器。
func (s *service) exportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p exportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	owner, err := s.ownerActor(ctx, p.ActorID)
	if err != nil {
		return job.ResultFile{}, err
	}
	tx, err := s.filtered(ctx, p.query(), owner)
	if err != nil {
		return job.ResultFile{}, err
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	total = min(total, asyncExportMax)
	report(0, fmt.Sprintf("共 %d 行", total))

	name := fmt.Sprintf("users-%s.xlsx", time.Now().Format("20060102"))
	return job.WriteResult(ctx, j, name, xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("用户", excel.SheetOptions{Header: exportHeader})
		if err != nil {
			return err
		}
		var done int64
		last := int64(math.MaxInt64)
		for done < total {
			if err := ctx.Err(); err != nil {
				return err
			}
			var rows []User
			if err := tx.Where("id < ?", last).Order("id DESC").Limit(int(min(exportBatch, total-done))).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			dtos, err := s.toDTOs(ctx, rows)
			if err != nil {
				return err
			}
			for _, u := range dtos {
				if err := writeUserRow(sh, u); err != nil {
					return err
				}
			}
			done += int64(len(rows))
			last = int64(rows[len(rows)-1].ID)
			report(int(done*100/max(total, 1)), fmt.Sprintf("已导出 %d / %d 行", done, total))
		}
		return x.Close()
	})
}

// ownerActor 在执行时按任务所有者重新取身份：用户已删除或停用则拒绝；Super 取当前 is_super，而不是提交时的快照。
// ctx 带任务所属租户（job 管理器注入），跨租户的 ActorID 查不到，同样拒绝。
func (s *service) ownerActor(ctx context.Context, id idgen.ID) (actor, error) {
	if id == 0 {
		return actor{}, job.Errorf("任务所有者无效")
	}
	u, err := s.find(ctx, id)
	if err != nil || u.Status != statusOn {
		return actor{}, job.Errorf("任务所有者不存在或已停用，无法导出")
	}
	return actor{ID: u.ID, Super: u.IsSuper}, nil
}

// asyncHandler 提供 POST /users/export-async。
type asyncHandler struct {
	svc *service
}

type filterBody struct {
	Keyword     string `json:"keyword"`
	Status      string `json:"status"`
	DeptID      string `json:"deptId"`
	Gender      string `json:"gender"`
	CreatedFrom string `json:"createdFrom"`
	CreatedTo   string `json:"createdTo"`
}

// filterQuery 取筛选条件：查询参数与列表接口相同；POST 时也接受同名 JSON 字段（非空者优先）。
func filterQuery(c *gin.Context) listQuery {
	q := queryOf(c)
	if c.Request.Method == "POST" && c.Request.ContentLength != 0 {
		var b filterBody
		if c.ShouldBindJSON(&b) == nil {
			pick := func(dst *string, v string) {
				if v != "" {
					*dst = v
				}
			}
			pick(&q.Keyword, b.Keyword)
			pick(&q.CreatedFrom, b.CreatedFrom)
			pick(&q.CreatedTo, b.CreatedTo)
			// 多值字段（body 里同样是逗号拼接串）：非空才覆盖 query 解析结果
			if s := response.ParseList(b.Status, response.MaxFilterValues); len(s) > 0 {
				q.Status = s
			}
			if ids := response.ParseFilterIDs(b.DeptID, response.MaxFilterValues); len(ids) > 0 {
				q.DeptIDs = ids
			}
			if s := response.ParseList(b.Gender, response.MaxFilterValues); len(s) > 0 {
				q.Gender = s
			}
		}
	}
	return q
}

// create POST /users/export-async：创建导出任务并立即返回任务 ID。
func (h *asyncHandler) create(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("user.export_center_disabled"))
		return
	}
	q := filterQuery(c)
	who := me(c)
	// 先校验筛选条件（日期格式、部门 ID），避免任务提交后才失败
	if _, err := h.svc.filtered(c.Request.Context(), q, who); err != nil {
		response.Fail(c, err)
		return
	}
	deptCSV := make([]string, 0, len(q.DeptIDs))
	for _, id := range q.DeptIDs {
		deptCSV = append(deptCSV, idgen.ID(id).String())
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{
		Type: jobTypeUserExport, Owner: who.ID,
		Params: exportParams{
			Keyword: q.Keyword, Status: strings.Join(q.Status, ","), DeptID: strings.Join(deptCSV, ","), Gender: strings.Join(q.Gender, ","),
			CreatedFrom: q.CreatedFrom, CreatedTo: q.CreatedTo,
			ActorID: who.ID, Super: who.Super,
		},
	})
	switch {
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(429, "error.too_many"))
		return
	case err != nil:
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
}
