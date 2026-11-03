package region

// 区划码逐级回退解析。身份证、永居证等证件的前 6 位都要靠它拿省/市/县名称。

// Trail 按省 → 地 → 县逐级回退，返回最深命中的名称路径与层级（1 省 / 2 地 / 3 县）。
//
// 刻意不要求"必须命中县级"：区划快照粒度不一、撤县设区、快照年份滞后都会让 6 位码
// 只认到上级；调用方需要把这种情况当信息缺失，而不是把数据本身判成非法。
// 查不到的层级直接跳过，全部查不到返回 (nil, 0)。
func Trail(code string) (path []string, level int) {
	if len(code) != 6 {
		return nil, 0
	}
	levels := []string{code[:2] + "0000", code[:4] + "00", code}
	for i, c := range levels {
		if i > 0 && c == levels[i-1] {
			continue // 直辖市、省直辖县级单位：上级与本级代码同形
		}
		name, ok := Name(c)
		if !ok {
			if h, historical := HistoricalInfo(c); historical {
				name = h.Name
			} else {
				continue
			}
		}
		if name == "" {
			continue
		}
		if len(path) > 0 && path[len(path)-1] == name {
			continue // 同名层级不重复列
		}
		path = append(path, name)
		level = i + 1
	}
	return path, level
}
