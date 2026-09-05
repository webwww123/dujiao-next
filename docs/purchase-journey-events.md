# 购买诊断采集契约

journey_v1 新接收：product_impression、quick_buy_click、quick_buy_close、checkout_exit、payment_exit、checkout_resume、payment_resume。已有 quick_buy_open 在带 instrumentation_version=journey_v1 时使用同一严格字段白名单；旧版请求保持原兼容行为。

这些事件只能表达浏览器看到的状态，不是后台交易终态。字段清洗在 RecordBatch 执行，先清掉游客邮箱、优惠码和元素文字，再做库存券查询与入库；保留安全的商品/订单/支付 ID 与两层实验标识。新事件中的 URL 不保留用户信息、查询或 fragment；referrer 仅保留 origin；错误只记结构化标志。

客户端 payment_paid_success、order_fulfill_success 仍不在白名单中。原有 behaviorSessionPaid 会参考旧 payment_success 事件；正式实验复盘必须另对真实实付订单核对，不能将旧行为看板直接用作成交权威。本次不改历史报表分类逻辑。

无 schema 变更，无订单、支付通道、券配置或生产数据库写入。上线需要先发布后端白名单，再发布前端；本轮未部署。后端仓库已有其他未推送提交，不得整批随本改动上线。

本地验证：go test ./internal/service -run TestBehaviorAnalytics -count=1 -timeout 120s。使用独立内存 SQLite 测试库，覆盖新事件接收、双层实验字段保留、敏感字段去除和非法终态拒绝；不访问生产订单。
