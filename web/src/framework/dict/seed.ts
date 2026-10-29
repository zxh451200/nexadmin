import type { DictItem } from '@/framework/request/types'

export interface DictSeedItem extends DictItem {
  /** 英文标签，界面语言为 en* 时使用（老字段，真实数据里正在被 i18nJson 取代） */
  labelEn?: string
  /** 与后端同一份契约：{"ja-JP":{"label":"…"}}，优先于 labelEn */
  i18nJson?: string
}

export interface DictSeedType {
  name: string
  items: DictSeedItem[]
}

export const dictSeed: Record<string, DictSeedType> = {
  sys_status: {
    name: '通用状态',
    items: [
      { value: '1', label: '启用', labelEn: 'Enabled', tag: 'success' },
      { value: '0', label: '停用', labelEn: 'Disabled', tag: 'default' },
    ],
  },
  sys_gender: {
    name: '性别',
    items: [
      { value: '0', label: '未知', labelEn: 'Unknown', tag: 'default' },
      // 第三条语言用 i18nJson 演示：mock 与后端同一条取值链（i18nJson → labelEn → label）
      { value: '1', label: '男', labelEn: 'Male', i18nJson: '{"ja-JP":{"label":"男性"},"en-US":{"label":"Male"}}', tag: 'info' },
      { value: '2', label: '女', labelEn: 'Female', tag: 'warning' },
    ],
  },
  yes_no: {
    name: '是否',
    items: [
      { value: '1', label: '是', labelEn: 'Yes', tag: 'success' },
      { value: '0', label: '否', labelEn: 'No', tag: 'default' },
    ],
  },
  notice_type: {
    name: '公告类型',
    items: [
      { value: 'notice', label: '通知', labelEn: 'Notice', tag: 'info' },
      { value: 'announcement', label: '公告', labelEn: 'Announcement', tag: 'warning' },
      { value: 'activity', label: '活动', labelEn: 'Activity', tag: 'success' },
    ],
  },
  notice_level: {
    name: '公告级别',
    items: [
      { value: 'normal', label: '普通', labelEn: 'Normal', tag: 'default' },
      { value: 'important', label: '重要', labelEn: 'Important', tag: 'destructive' },
    ],
  },
  notice_status: {
    name: '公告状态',
    items: [
      { value: 'draft', label: '草稿', labelEn: 'Draft', tag: 'default' },
      { value: 'published', label: '已发布', labelEn: 'Published', tag: 'success' },
      { value: 'revoked', label: '已撤回', labelEn: 'Revoked', tag: 'warning' },
    ],
  },
  message_category: {
    name: '消息分类',
    items: [
      { value: 'system', label: '系统消息', labelEn: 'System', tag: 'info' },
      { value: 'business', label: '业务消息', labelEn: 'Business', tag: 'success' },
      { value: 'security', label: '安全提醒', labelEn: 'Security', tag: 'destructive' },
      { value: 'todo', label: '待办提醒', labelEn: 'To-do', tag: 'warning' },
    ],
  },
  oper_type: {
    name: '操作类型',
    items: [
      { value: 'create', label: '新增', labelEn: 'Create', tag: 'success' },
      { value: 'update', label: '修改', labelEn: 'Update', tag: 'info' },
      { value: 'delete', label: '删除', labelEn: 'Delete', tag: 'destructive' },
      { value: 'export', label: '导出', labelEn: 'Export', tag: 'default' },
      { value: 'import', label: '导入', labelEn: 'Import', tag: 'default' },
      { value: 'grant', label: '授权', labelEn: 'Grant', tag: 'warning' },
      { value: 'sort', label: '调整顺序', labelEn: 'Reorder', tag: 'default' },
      { value: 'other', label: '其他', labelEn: 'Other', tag: 'default' },
    ],
  },
  login_status: {
    name: '登录状态',
    items: [
      { value: 'success', label: '成功', labelEn: 'Success', tag: 'success' },
      { value: 'failed', label: '失败', labelEn: 'Failed', tag: 'destructive' },
    ],
  },
  menu_type: {
    name: '菜单类型',
    items: [
      { value: 'dir', label: '目录', labelEn: 'Directory', tag: 'default' },
      { value: 'menu', label: '菜单', labelEn: 'Menu', tag: 'info' },
      { value: 'button', label: '按钮', labelEn: 'Button', tag: 'warning' },
    ],
  },
  data_scope: {
    name: '数据范围',
    items: [
      { value: 'all', label: '全部数据', labelEn: 'All data' },
      { value: 'custom', label: '自定义部门', labelEn: 'Custom departments' },
      { value: 'dept', label: '本部门', labelEn: 'Own department' },
      { value: 'deptAndChild', label: '本部门及以下', labelEn: 'Department and below' },
      { value: 'self', label: '仅本人', labelEn: 'Self only' },
    ],
  },
  tenant_status: {
    name: '租户状态',
    items: [
      { value: 'active', label: '正常', labelEn: 'Active', tag: 'success' },
      { value: 'suspended', label: '已停用', labelEn: 'Suspended', tag: 'warning' },
      { value: 'expired', label: '已过期', labelEn: 'Expired', tag: 'destructive' },
    ],
  },
  online_device: {
    name: '在线设备',
    items: [
      { value: 'web', label: '网页端', labelEn: 'Web', tag: 'info' },
      { value: 'mobile', label: '手机', labelEn: 'Mobile', tag: 'success' },
      { value: 'tablet', label: '平板', labelEn: 'Tablet', tag: 'warning' },
      { value: 'other', label: '其他', labelEn: 'Other', tag: 'default' },
    ],
  },
}
