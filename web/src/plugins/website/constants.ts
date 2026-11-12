/**
 * 产品贸易字段的固定选项：币种 / 装运港口 / 付款方式。
 * 值（落库、同步阿里、官网展示）一律用行业通用英文写法；界面显示名走 i18n
 * （plugin_website.port/payment/unit.<值>），中文界面显示「值 · 中文名」，英文界面只显示值。
 * 币种名称用 Intl.DisplayNames 现算（随界面语言）。
 */

/** 常用结算币种（ISO 4217），按外贸场景常见度排列 */
export const CURRENCIES = [
  'USD', 'CNY', 'EUR', 'GBP', 'JPY', 'KRW', 'HKD', 'TWD', 'SGD', 'MYR',
  'THB', 'VND', 'IDR', 'PHP', 'INR', 'AUD', 'NZD', 'CAD', 'CHF', 'RUB',
  'AED', 'SAR', 'TRY', 'ZAR', 'BRL', 'MXN', 'PLN', 'SEK', 'NOK', 'DKK',
] as const

/** 常用装运港口（外贸主要启运/中转港），下拉可搜 */
export const PORTS = [
  // 中国大陆
  'Shanghai', 'Shenzhen', 'Ningbo', 'Guangzhou', 'Qingdao', 'Tianjin', 'Xiamen', 'Dalian',
  'Lianyungang', 'Suzhou', 'Nanjing', 'Yingkou', 'Fuzhou', 'Zhanjiang', 'Beihai',
  // 港台及近洋
  'Hong Kong', 'Kaohsiung', 'Busan', 'Inchon', 'Tokyo', 'Yokohama', 'Nagoya', 'Osaka',
  'Vladivostok',
  // 东南亚 / 南亚 / 中东
  'Singapore', 'Port Klang', 'Tanjung Pelepas', 'Laem Chabang', 'Ho Chi Minh City', 'Manila',
  'Jakarta', 'Surabaya', 'Colombo', 'Mumbai', 'Nhava Sheva', 'Chennai', 'Kolkata', 'Karachi',
  'Jebel Ali', 'Dammam', 'Doha', 'Jeddah', 'Bandar Abbas',
  // 欧洲 / 地中海 / 黑海
  'Rotterdam', 'Antwerp', 'Hamburg', 'Bremen', 'Felixstowe', 'Southampton', 'Le Havre',
  'Marseille', 'Barcelona', 'Valencia', 'Genoa', 'La Spezia', 'Piraeus', 'Istanbul',
  'Constanta', 'Odessa', 'Gdansk', 'Copenhagen', 'Gothenburg', 'Oslo', 'Klaipeda', 'Riga',
  // 美洲 / 非洲 / 大洋洲
  'New York', 'Savannah', 'Long Beach', 'Los Angeles', 'Oakland', 'Seattle', 'Houston',
  'Charleston', 'Norfolk', 'Miami', 'Veracruz', 'Manzanillo', 'Santos', 'Buenos Aires',
  'Callao', 'Guayaquil', 'Toronto', 'Vancouver', 'Durban', 'Lagos', 'Mombasa', 'Tema',
  'Casablanca', 'Alexandria', 'Sydney', 'Melbourne', 'Brisbane', 'Fremantle', 'Auckland',
] as const

/** 常用付款方式（外贸标准缩写） */
export const PAYMENT_TERMS = ['T/T', 'L/C', 'D/P', 'D/A', 'O/A', 'Western Union', 'PayPal', 'MoneyGram', 'Escrow', 'Cash', 'Credit Card'] as const

/** 常用计量单位（计件/计重/计长/容积），外贸报价单常见写法 */
export const UNITS = [
  'piece', 'set', 'unit', 'pair', 'dozen', 'roll', 'sheet', 'bag', 'box', 'carton',
  'pallet', 'barrel', 'kg', 'g', 'ton', 'lb', 'm', 'cm', 'mm', 'm²', 'm³', 'L', 'mL',
] as const
