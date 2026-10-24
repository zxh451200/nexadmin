/* eslint-disable -- 本文件中的十六进制色值是「可选数据值」（用户选中后存入业务数据），不是界面样式，故不受颜色 token 规则约束 */

/** 预设色板（数据值） */
export const PRESET_COLORS: string[] = [
  '#ef4444', '#f97316', '#f59e0b', '#eab308', '#84cc16', '#22c55e', '#10b981', '#14b8a6',
  '#06b6d4', '#0ea5e9', '#3b82f6', '#6366f1', '#8b5cf6', '#a855f7', '#d946ef', '#ec4899',
  '#f43f5e', '#64748b', '#334155', '#000000', '#ffffff',
]

/** 原生取色器未选值时的默认值（数据值） */
export const FALLBACK_COLOR = '#000000'

/** 精选 lucide 图标名（PascalCase），约 80 个 */
export const ICON_NAMES: string[] = [
  'House', 'LayoutDashboard', 'User', 'Users', 'UserCog', 'Settings', 'Shield', 'ShieldCheck', 'Lock', 'Key',
  'Mail', 'Bell', 'Calendar', 'Clock', 'File', 'FileText', 'Folder', 'FolderOpen', 'Image', 'Video',
  'Music', 'Camera', 'Map', 'MapPin', 'Globe', 'Search', 'Star', 'Heart', 'Bookmark', 'Tag',
  'Flag', 'Link', 'Paperclip', 'Download', 'Upload', 'Share2', 'Send', 'MessageSquare', 'Phone', 'Database',
  'Server', 'Cloud', 'Cpu', 'Terminal', 'Code', 'Bug', 'Wrench', 'Hammer', 'Package', 'Box',
  'Truck', 'ShoppingCart', 'CreditCard', 'Wallet', 'DollarSign', 'ChartColumn', 'ChartPie', 'ChartLine', 'Activity', 'Gauge',
  'List', 'ListTree', 'Menu', 'Grid2x2', 'Layers', 'Building2', 'Briefcase', 'Award', 'Trophy', 'Gift',
  'Lightbulb', 'Zap', 'Rocket', 'Palette', 'Languages', 'Megaphone', 'ClipboardList', 'ScrollText', 'History', 'CircleHelp',
]

/** ISO 3166-1 alpha-2 国家/地区代码 */
export const COUNTRY_CODES: string[] =
  'AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW'.split(
    ' ',
  )
