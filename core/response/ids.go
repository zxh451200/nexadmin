package response

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/pkg/idgen"
)

// MaxBatchIDs 是 ?ids= 一次允许点名的上限。
//
// 批量删除的 IN (...) 会把每个 id 变成一个绑定参数：勾选不分页（前端跨页保留选中）或脚本
// 直接拼 URL 时，一条语句可能带上几千个参数，MySQL 的占位符上限与 PG 的语句长度都会先把请求
// 打成 500，而不是一句可读的「一次删太多了」。500 沿用 modules/file 自己已知的阈值。
const MaxBatchIDs = 500

// ParseIDs 解析 ?ids=1,2,3 这种批量入参。
//
// 三条硬规则，都是为了批量删除不越权：
//   - 任一项非法就整体拒绝，不做「删掉能删的」——部分成功会让调用方以为自己删的就是那几条；
//   - 解析不出任何 ID 就报错，绝不返回空列表。空列表喂给 `id IN ?` 是一条不打对象的语句，
//     而喂给「不带 ids 即全量操作」的老路由则是整张表的灾难；
//   - 超过 MaxBatchIDs 就报错，宁可拒绝，也不让一条超大 IN (...) 变成 500。
func ParseIDs(raw string) ([]idgen.ID, error) {
	var ids []idgen.ID
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		id, err := idgen.Parse(part)
		if err != nil || id <= 0 {
			return nil, BadRequest("error.bad_request")
		}
		ids = append(ids, id)
		if len(ids) > MaxBatchIDs {
			return nil, BadRequest("error.batch_too_many", MaxBatchIDs)
		}
	}
	if len(ids) == 0 {
		return nil, BadRequest("error.bad_request")
	}
	return ids, nil
}

// ParseIDsQuery 读当前请求的 ids 查询参数并按 ParseIDs 校验。
func ParseIDsQuery(c *gin.Context) ([]idgen.ID, error) {
	return ParseIDs(c.Query("ids"))
}
