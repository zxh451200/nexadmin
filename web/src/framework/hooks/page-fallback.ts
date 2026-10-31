/**
 * 删除后应停留的页码：当前页的行被全部删掉时回退一页，否则停在原页。
 * 只统计实际出现在当前页上的 id，跨页选中不会误触发回退。
 */
export function pageAfterDelete(page: number, pageRows: number, removedOnPage: number) {
  return page > 1 && pageRows > 0 && removedOnPage >= pageRows ? page - 1 : page
}
