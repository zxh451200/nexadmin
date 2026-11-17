import { registerMapProvider } from '@/framework/map/registry'
import { AmapPicker } from './AmapPicker'
import { BaiduPicker } from './BaiduPicker'
import { GooglePicker } from './GooglePicker'
import { OsmPicker } from './OsmPicker'
import { TiandituPicker } from './TiandituPicker'

registerMapProvider({ id: 'amap', title: '高德地图', Picker: AmapPicker })
registerMapProvider({ id: 'baidu', title: '百度地图', Picker: BaiduPicker })
registerMapProvider({ id: 'google', title: 'Google 地图', Picker: GooglePicker })
registerMapProvider({ id: 'osm', title: 'OpenStreetMap', Picker: OsmPicker })
registerMapProvider({ id: 'tianditu', title: '天地图', Picker: TiandituPicker })
