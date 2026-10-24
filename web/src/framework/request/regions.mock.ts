import { defineMock } from './mock'
import type { RegionNode } from './types'

// [code, name, pinyin, children?]
type Raw = [string, string, string, Raw[]?]

const raw: Raw[] = [
  ['110000', '北京市', 'beijing', [
    ['110100', '市辖区', 'shixiaqu', [
      ['110101', '东城区', 'dongcheng'],
      ['110102', '西城区', 'xicheng'],
      ['110105', '朝阳区', 'chaoyang'],
      ['110106', '丰台区', 'fengtai'],
      ['110108', '海淀区', 'haidian'],
    ]],
  ]],
  ['320000', '江苏省', 'jiangsu', [
    ['320100', '南京市', 'nanjing', [
      ['320102', '玄武区', 'xuanwu'],
      ['320104', '秦淮区', 'qinhuai'],
      ['320105', '建邺区', 'jianye'],
      ['320106', '鼓楼区', 'gulou'],
    ]],
    ['320200', '无锡市', 'wuxi', [
      ['320205', '锡山区', 'xishan'],
      ['320206', '惠山区', 'huishan'],
      ['320211', '滨湖区', 'binhu'],
    ]],
    ['320500', '苏州市', 'suzhou', [
      ['320505', '虎丘区', 'huqiu'],
      ['320506', '吴中区', 'wuzhong'],
      ['320507', '相城区', 'xiangcheng'],
      ['320508', '姑苏区', 'gusu'],
    ]],
  ]],
  ['330000', '浙江省', 'zhejiang', [
    ['330100', '杭州市', 'hangzhou', [
      ['330102', '上城区', 'shangcheng'],
      ['330105', '拱墅区', 'gongshu'],
      ['330106', '西湖区', 'xihu'],
      ['330108', '滨江区', 'binjiang'],
      ['330109', '萧山区', 'xiaoshan'],
      ['330110', '余杭区', 'yuhang'],
    ]],
    ['330200', '宁波市', 'ningbo', [
      ['330203', '海曙区', 'haishu'],
      ['330205', '江北区', 'jiangbei'],
      ['330206', '北仑区', 'beilun'],
      ['330212', '鄞州区', 'yinzhou'],
    ]],
    ['330300', '温州市', 'wenzhou', [
      ['330302', '鹿城区', 'lucheng'],
      ['330303', '龙湾区', 'longwan'],
      ['330304', '瓯海区', 'ouhai'],
    ]],
  ]],
  ['440000', '广东省', 'guangdong', [
    ['440100', '广州市', 'guangzhou', [
      ['440103', '荔湾区', 'liwan'],
      ['440104', '越秀区', 'yuexiu'],
      ['440105', '海珠区', 'haizhu'],
      ['440106', '天河区', 'tianhe'],
      ['440111', '白云区', 'baiyun'],
    ]],
    ['440300', '深圳市', 'shenzhen', [
      ['440303', '罗湖区', 'luohu'],
      ['440304', '福田区', 'futian'],
      ['440305', '南山区', 'nanshan'],
      ['440306', '宝安区', 'baoan'],
      ['440307', '龙岗区', 'longgang'],
      ['440308', '盐田区', 'yantian'],
    ]],
    ['440400', '珠海市', 'zhuhai', [
      ['440402', '香洲区', 'xiangzhou'],
      ['440403', '斗门区', 'doumen'],
      ['440404', '金湾区', 'jinwan'],
    ]],
  ]],
  ['510000', '四川省', 'sichuan', [
    ['510100', '成都市', 'chengdu', [
      ['510104', '锦江区', 'jinjiang'],
      ['510105', '青羊区', 'qingyang'],
      ['510106', '金牛区', 'jinniu'],
      ['510107', '武侯区', 'wuhou'],
      ['510108', '成华区', 'chenghua'],
    ]],
    ['510700', '绵阳市', 'mianyang', [
      ['510703', '涪城区', 'fucheng'],
      ['510704', '游仙区', 'youxian'],
      ['510705', '安州区', 'anzhou'],
    ]],
  ]],
]

function build(list: Raw[], depth: number, level: number): RegionNode[] {
  return list.map(([code, name, pinyin, children]) => {
    const node: RegionNode = { code, name, pinyin }
    if (children && depth < level) node.children = build(children, depth + 1, level)
    return node
  })
}

defineMock([
  {
    method: 'GET',
    url: '/regions/tree',
    handler: ({ query }) => build(raw, 1, Math.min(3, Math.max(1, Number(query.level) || 3))),
  },
])
