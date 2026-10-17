package remote

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"regexp"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/secretstore"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

type Mutation struct {
	Client
	ExpectedEndpoint  string `json:"-"`
	ExpectedTag       string `json:"-"`
	ExpectedProtocol  string `json:"-"`
	Action            string `json:"action"`
	Revision          int64  `json:"revision"`
	RestoreAuthorized bool   `json:"restore_authorized"`
}
type MutationResult struct {
	Status      string `json:"status"`
	Persistence string `json:"persistence"`
	Revision    int64  `json:"revision"`
	Reason      string `json:"reason"`
}

func runtimeTarget(ctx context.Context, s *storage.Store, inbound string) (core.Instance, string, string, error) {
	var i core.Instance
	var tag, protocol string
	e := s.DB.QueryRowContext(ctx, `SELECT x.id,x.server_id,x.core_type,x.api_endpoint,x.enabled,c.tag,c.protocol FROM catalog_inbounds c JOIN instances x ON x.id=c.instance_id JOIN servers v ON v.id=x.server_id WHERE c.id=? AND c.retired=0 AND v.enabled=1`, inbound).Scan(&i.ID, &i.ServerID, &i.CoreType, &i.APIEndpoint, &i.Enabled, &tag, &protocol)
	if e == nil && !i.Enabled {
		e = fmt.Errorf("实例已停用")
	}
	return i, tag, protocol, e
}
func runtimeAccount(kind, protocol string, d Client) (*hp.TypedAccount, error) {
	prefix := "xray"
	if kind == "v2fly" {
		prefix = "v2ray.core"
	}
	var message proto.Message
	switch protocol {
	case "vless", "vmess":
		if !regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(d.UUID) {
			return nil, fmt.Errorf("UUID 无效")
		}
		if d.Flow != "" && (protocol != "vless" || d.Flow != "xtls-rprx-vision") {
			return nil, fmt.Errorf("不支持的 Flow")
		}
		message = &hp.UUIDAccount{Id: d.UUID, Flow: d.Flow}
	case "trojan":
		if d.Password == "" || len(d.Password) > 4096 {
			return nil, fmt.Errorf("密码无效")
		}
		message = &hp.PasswordAccount{Password: d.Password}
	default:
		return nil, fmt.Errorf("当前协议未实现安全运行时用户写入")
	}
	b, e := proto.Marshal(message)
	return &hp.TypedAccount{Type: prefix + ".proxy." + protocol + ".Account", Value: b}, e
}
func dialRuntime(ctx context.Context, i core.Instance, allowed []string) (*grpc.ClientConn, error) {
	p := (security.Policy{Allowed: allowed}).ForEndpoints(i.APIEndpoint)
	if e := p.Check(ctx, security.TargetAddress(i.APIEndpoint)); e != nil {
		return nil, e
	}
	return grpc.NewClient("passthrough:///"+i.APIEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(p.Dial))
}
func alter(ctx context.Context, conn *grpc.ClientConn, kind, tag, action string, user *hp.User) error {
	prefix := "xray"
	if kind == "v2fly" {
		prefix = "v2ray.core"
	}
	var value proto.Message
	name := "AddUserOperation"
	if action == "remove" {
		name = "RemoveUserOperation"
		value = &hp.RemoveUserOperation{Email: user.Email}
	} else {
		value = &hp.AddUserOperation{User: user}
	}
	b, e := proto.Marshal(value)
	if e != nil {
		return e
	}
	return conn.Invoke(ctx, "/"+prefix+".app.proxyman.command.HandlerService/AlterInbound", &hp.AlterInboundRequest{Tag: tag, Operation: &hp.TypedAccount{Type: prefix + ".app.proxyman.command." + name, Value: b}}, &hp.AlterInboundResponse{})
}
func readUser(ctx context.Context, c *grpc.ClientConn, tag, ref string) (*hp.User, error) {
	r, e := hp.NewHandlerServiceClient(c).GetInboundUsers(ctx, &hp.GetInboundUserRequest{Tag: tag, Email: ref})
	if e != nil {
		return nil, e
	}
	for _, u := range r.Users {
		if u != nil && u.Email == ref {
			return u, nil
		}
	}
	return nil, nil
}

// Mutate is an explicit administrator action, never called by diagnostics or
// reset preparation. Desired authentication is sealed before any remote write.
func Mutate(ctx context.Context, s *storage.Store, keyFile string, allowed []string, d Mutation) (MutationResult, error) {
	result := MutationResult{Persistence: "runtime_only"}
	if !d.Confirmed || d.Reference == "" || len(d.Reference) > 200 || strings.ContainsAny(d.Reference, "\r\n\x00") || strings.Contains(d.Reference, ">>>") {
		return result, fmt.Errorf("运行时变更需要确认真实身份与连接影响")
	}
	if d.Action != "add" && d.Action != "remove" && d.Action != "update" {
		return result, fmt.Errorf("不允许的用户操作")
	}
	i, tag, protocol, e := runtimeTarget(ctx, s, d.InboundID)
	if e != nil {
		return result, e
	}
	if d.ExpectedEndpoint != "" && (i.APIEndpoint != d.ExpectedEndpoint || tag != d.ExpectedTag || protocol != d.ExpectedProtocol) {
		return result, fmt.Errorf("用户创建目标已变化，拒绝运行时写入")
	}
	if i.CoreType != "xray" && i.CoreType != "v2fly" {
		return result, fmt.Errorf("当前官方 API 无服务端用户写入；不会改写核心配置")
	}
	if i.CoreType == "v2fly" && (d.Action == "update" || d.RestoreAuthorized) {
		return result, fmt.Errorf("V2Fly 无用户回读枚举，不能安全自动替换或恢复；请明确添加/删除")
	}
	key, e := secretstore.Key(keyFile)
	if e != nil {
		return result, fmt.Errorf("运行时 Desired 需要先配置 TML_MANAGED_KEY_FILE（独立的私有 32 字节密钥）")
	}
	var account *hp.TypedAccount
	if d.Action != "remove" {
		account, e = runtimeAccount(i.CoreType, protocol, d.Client)
		if e != nil {
			return result, e
		}
	}
	raw, _ := json.Marshal(d.Client)
	sealed, e := secretstore.Seal(key, raw)
	if e != nil {
		return result, e
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	var revision int64
	var previous string
	e = tx.QueryRowContext(ctx, "SELECT revision,status FROM remote_runtime_users WHERE inbound_id=? AND client_ref=?", d.InboundID, d.Reference).Scan(&revision, &previous)
	if e != nil && e != sql.ErrNoRows {
		return result, e
	}
	if revision != d.Revision || previous == "applying" || previous == "restoring" {
		return result, fmt.Errorf("Desired 修订变化或操作未完成，请重新检查运行状态")
	}
	result.Revision = revision + 1
	_, e = tx.ExecContext(ctx, `INSERT INTO remote_runtime_users(inbound_id,client_ref,desired_sealed,desired_enabled,restore_authorized,api_endpoint,core_tag,protocol,revision,status,checked_at) VALUES(?,?,?,?,?,?,?,?,?,'applying',?) ON CONFLICT(inbound_id,client_ref) DO UPDATE SET desired_sealed=excluded.desired_sealed,desired_enabled=excluded.desired_enabled,restore_authorized=excluded.restore_authorized,api_endpoint=excluded.api_endpoint,core_tag=excluded.core_tag,protocol=excluded.protocol,revision=excluded.revision,status='applying',checked_at=excluded.checked_at`, d.InboundID, d.Reference, sealed, d.Action != "remove", d.RestoreAuthorized, i.APIEndpoint, tag, protocol, result.Revision, storage.Stamp(time.Now()))
	if e != nil {
		return result, e
	}
	if e = tx.Commit(); e != nil {
		return result, e
	}
	finish := func(state, reason string) (MutationResult, error) {
		persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		res, err := s.DB.ExecContext(persist, "UPDATE remote_runtime_users SET status=?,checked_at=? WHERE inbound_id=? AND client_ref=? AND revision=?", state, storage.Stamp(time.Now()), d.InboundID, d.Reference, result.Revision)
		if err != nil {
			return result, fmt.Errorf("远程操作状态未能持久保存，请回读核查，禁止盲目重试")
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return result, fmt.Errorf("操作修订冲突，请核查运行状态")
		}
		result.Status, result.Reason = state, reason
		return result, nil
	}
	c, e := dialRuntime(ctx, i, allowed)
	if e != nil {
		return finish("unknown", "API 连接失败；未声称成功")
	}
	defer c.Close()
	user := &hp.User{Email: d.Reference, Account: account, Level: d.Level}
	if i.CoreType == "xray" {
		old, err := readUser(ctx, c, tag, d.Reference)
		if err != nil {
			return finish("unknown", "不能回读实际用户；拒绝变更")
		}
		if old != nil {
			user.Level = old.Level
			d.Level = old.Level
			material, _ := json.Marshal(d.Client)
			seal, err := secretstore.Seal(key, material)
			if err != nil {
				return finish("unknown", "Desired 级别保存失败，未写核心")
			}
			if _, err = s.DB.ExecContext(ctx, "UPDATE remote_runtime_users SET desired_sealed=? WHERE inbound_id=? AND client_ref=? AND revision=?", seal, d.InboundID, d.Reference, result.Revision); err != nil {
				return finish("unknown", "Desired 级别保存失败，未写核心")
			}
		}
		if old != nil && d.Action == "add" && !proto.Equal(old.Account, account) {
			return finish("conflict", "已存在不同凭据，不自动覆盖")
		}
		if d.Action == "update" && old != nil && !proto.Equal(old.Account, account) {
			if err = alter(ctx, c, i.CoreType, tag, "remove", old); err != nil {
				return finish("unknown", "替换的移除阶段未确认："+status.Code(err).String())
			}
			if err = alter(ctx, c, i.CoreType, tag, "add", user); err != nil {
				restore, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				defer stop()
				rollback := alter(restore, c, i.CoreType, tag, "add", old)
				if rollback != nil {
					return finish("unknown", "替换失败且补偿未确认；需人工核查")
				}
				return finish("desired_mismatch", "新认证未应用；旧账号已补偿恢复，需重新核查")
			}
		} else if !(d.Action == "remove" && old == nil) && !(d.Action != "remove" && old != nil && proto.Equal(old.Account, account)) {
			if err = alter(ctx, c, i.CoreType, tag, d.Action, user); err != nil {
				return finish("unknown", "RPC 未确认："+status.Code(err).String())
			}
		}
		actual, err := readUser(ctx, c, tag, d.Reference)
		if err != nil {
			return finish("unknown", "操作返回后回读失败，不能证明最终状态")
		}
		if d.Action == "remove" && actual != nil || d.Action != "remove" && (actual == nil || !proto.Equal(actual.Account, account)) {
			return finish("desired_mismatch", "回读与 Desired 不一致")
		}
	} else if e = alter(ctx, c, i.CoreType, tag, d.Action, user); e != nil {
		return finish("unknown", "RPC 未确认："+status.Code(e).String())
	}
	if d.Action == "remove" {
		_, e = s.DB.ExecContext(ctx, `UPDATE credential_material SET status='runtime_removed' WHERE id IN(SELECT credential_id FROM inbound_client_keys WHERE inbound_id=? AND client_ref=?)`, d.InboundID, d.Reference)
	} else {
		e = func() error {
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if _, err = saveClient(ctx, tx, d.Client, "native_api"); err != nil {
				return err
			}
			return tx.Commit()
		}()
	}
	if e != nil {
		return finish("unknown", "运行时已变更，但本地客户端登记未完成；请核查")
	}
	if i.CoreType == "v2fly" {
		return finish("runtime_applied_unverified", "AlterInbound 返回成功；无完整用户回读，持久化未保证")
	}
	return finish("runtime_verified", "已回读验证；仅运行时生效，重启后需重新验证")
}
