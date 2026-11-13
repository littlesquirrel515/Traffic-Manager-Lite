# v2.5.9 流量用户运营名称

用户流量 GET /users 保留原 user ID、display_name 和汇总关系，新增只读 operator_name、name_source、subscriber_owners、identity_refs 与 partial_ownership。名字只从 subscriber_credentials 和 subscriber_usage_sources 的持久关系取得，精确匹配实例和真实 client_ref；绝不根据 tml-node/tml 前缀解码或猜测用户。

Xray/V2Fly/Hysteria2 的全局用户计数按实例+client_ref 关联。sing-box 等入站粒度必须匹配稳定入站当前/历史 Tag；空 Tag 不猜测跨入站同名归属。当前有效关联优先于已冻结历史关联，历史关联明确标记。多个用户共享原生账号时列出全部名字并说明共享总量，不拆分或重复累计。合并身份中只有部分得到归属时明确提示。

人工设置的流量显示别名优先，仍展示订阅用户归属；原生/未关联用户保留原显示名，支持“显示别名”。原技术标识在可展开详情中显示，搜索支持订阅用户名、别名、原标识和实例/Tag。用户趋势使用运营名称，改名只修改原统计用户显示名，不修改订阅用户或核心账号。

无迁移，不改用户/身份 ID、不创建 Client、不写名字、不轮换 Token、不变更授权或计费；按原统计身份分行，不合并同名订阅用户的多个流量行。测试覆盖跨实例/跨入站同名、共享、原生别名、历史归属、部分身份以及查询前后流量不变。
