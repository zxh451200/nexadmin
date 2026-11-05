# devicehub — 设备接入与监控（Pro）

工业设备接入、采集、监控、指令下发与历史数据。协议差异收敛在 driver 接口后面，
新增协议（二期候选：三菱 MC、欧姆龙 Fins）= 新增一个 driver 子包实现 + Register，不加架构。

## 支持的协议与地址语法

| 协议 | endpoint | 地址示例 | 说明 |
|---|---|---|---|
| modbus-tcp | `192.168.1.20:502` | `hr:40001` `ir:30001` `co:1` `di:1`（支持 `0x` 十六进制） | 变频器/PLC 通用从站口 |
| modbus-rtu | `COM3:9600,8,E,1` | 同上 | RS485 直连串口 |
| mqtt | `tcp://192.168.1.10:1883` | 相对主题 `motor/frequency` | 上行推送型；下行 publish 到 `{prefix}/down/{address}` |
| s7 | `192.168.1.30:102` | `DB1.DBX0.0` `DB1.DBW2` `DB1.DBD4`、`M0.0` `MW10` `MD20`、`I*` `Q*` `T*` `C*` | 西门子全系；rack/slot 在 config |
| opcua | `opc.tcp://192.168.1.40:4840` | `ns=2;s=Tag1` `ns=3;i=1001` | 安全策略/用户名密码在 config |

## 协议配置（device.config JSON）

```jsonc
// modbus-tcp / modbus-rtu
{"unitId":1,"pollIntervalMs":5000,"timeoutMs":2000}

// mqtt
{"username":"","password":"","upstreamPrefix":"devicehub/up/DEV1","offlineAfterSeconds":60}

// s7
{"rack":0,"slot":1,"pollIntervalMs":5000}   // 1200/1500 slot=0；300/400 slot=1 或 2

// opcua
{"securityPolicy":"None","username":"","password":"","timeoutMs":5000}
```

## MQTT 主题契约

- 上行（设备 → 平台）：`{upstreamPrefix}/{pointAddress}`，payload 为 `{"v":123.5}`、裸数字或裸字符串
- 下行（平台 → 设备）：`{upstreamPrefix}/down/{pointAddress}`，payload `{"v":<raw>}`（QoS1）
- 离线判定：broker 断开或上行静默超过 `offlineAfterSeconds`

## 数据与生命周期

- 采集落库带**死区压缩**（point.deadband，偏差内只刷最近值不落行）
- 原始读数保留 48h，内置 sweeper 每小时聚合进小时桶 `plg_devhub_reading_agg`，原始行滚动删除
- 设备删除级联清测点/读数/指令/告警规则；测点删除保留历史

## 权限码

- `devicehub:list` 查看（实时/历史/导出/配置读取 + 告警通知收件人）
- `devicehub:edit` 管理设备/测点/告警规则
- `devicehub:control` 指令下发（高危独立，操作日志必记）

## 宿主集成（进程内 API）

业务模块 import `nexadmin/plugins/devicehub`：

```go
devicehub.Latest(ctx, "DEV1", "motor/frequency")            // 最近值（联动判断）
devicehub.Points(ctx, "DEV1")                               // 全部测点最近值
devicehub.SendCommand(ctx, "DEV1", "motor/frequency", "350", "mes") // 按地址下发
devicehub.History(ctx, "DEV1", "motor/frequency", from, to, "hour") // 历史趋势
```

事件（core/event 订阅或 openapi webhook）：`devicehub.alarm` / `devicehub.command` / `devicehub.status`。

## 通知

阈值告警站内信发给持有 `devicehub:list` 的用户（按租户分组）。SMTP 由核心设置统一管理。
