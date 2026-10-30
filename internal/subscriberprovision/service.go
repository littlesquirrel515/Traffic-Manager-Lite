// Package subscriberprovision owns the subscriber-to-core provisioning workflow.
// Database commits and core RPCs are separated by a durable, retryable operation.
package subscriberprovision

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/hysteriaauth"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/secretstore"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

type Service struct {
	Store  *storage.Store
	Config config.Config
}
type Request struct {
	RequestID    string                  `json:"request_id"`
	Subscriber   subscription.Subscriber `json:"subscriber"`
	InboundID    string                  `json:"inbound_id"`
	CredentialID string                  `json:"credential_id"`
	Mode         string                  `json:"mode"`
	Confirmed    bool                    `json:"confirmed"`
}
type Result struct {
	SubscriberID string `json:"subscriber_id"`
	RequestID    string `json:"request_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

func (s Service) Create(ctx context.Context, d Request) (Result, error) {
	r := Result{RequestID: d.RequestID}
	if !d.Confirmed || len(d.RequestID) < 16 || len(d.RequestID) > 100 || strings.ContainsAny(d.RequestID, "\r\n\x00") {
		return r, fmt.Errorf("需要确认目标入站及用户业务，并提供稳定请求标识")
	}
	payload, _ := json.Marshal(d)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(payload))
	var existing, priorHash string
	e := s.Store.DB.QueryRowContext(ctx, "SELECT subscriber_id,request_hash FROM subscriber_provisioning WHERE request_id=?", d.RequestID).Scan(&existing, &priorHash)
	if e == nil {
		if priorHash != fingerprint {
			return r, fmt.Errorf("请求标识已用于其他参数，请重新打开表单")
		}
		return s.Retry(ctx, d.RequestID)
	}
	if e != sql.ErrNoRows {
		return r, e
	}
	var kind, protocol, endpoint, tag string
	var present, review, retired bool
	e = s.Store.DB.QueryRowContext(ctx, `SELECT x.core_type,b.protocol,x.api_endpoint,b.tag,b.present,b.review,b.retired FROM catalog_inbounds b JOIN instances x ON x.id=b.instance_id WHERE b.id=?`, d.InboundID).Scan(&kind, &protocol, &endpoint, &tag, &present, &review, &retired)
	if e != nil || !present || review || retired {
		return r, fmt.Errorf("请选择入站管理中有效的稳定 Inbound")
	}
	if d.Mode != "managed" && d.Mode != "native" {
		return r, fmt.Errorf("请选择新建动态客户端或纳管已有原生客户端")
	}
	var client remote.Client
	sealed := ""
	if d.Mode == "managed" {
		if kind != "xray" && kind != "hysteria2" {
			return r, fmt.Errorf("当前 %s 适配器没有经过回读验证的动态用户添加/恢复接口；不能创建虚假已生效用户", kind)
		}
		if kind == "hysteria2" && (protocol != "hysteria2" || !hysteriaauth.Ready(ctx, s.Store, d.InboundID)) {
			return r, fmt.Errorf("请先准备 Hysteria2 HTTP 认证后端，并明确确认核心已经配置使用该后端；静态 userpass 不能动态添加")
		}
		if kind == "xray" && protocol != "vless" && protocol != "vmess" && protocol != "trojan" {
			return r, fmt.Errorf("此协议尚未支持经验证的动态客户端创建")
		}
		key, err := secretstore.Key(s.Config.ManagedKeyFile)
		if err != nil {
			return r, fmt.Errorf("请先配置 TML_MANAGED_KEY_FILE，动态用户 Desired 必须加密保存")
		}
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			return r, err
		}
		b[6] = (b[6] & 15) | 64
		b[8] = (b[8] & 63) | 128
		h := hex.EncodeToString(b)
		client = remote.Client{InboundID: d.InboundID, Reference: "tml-" + d.RequestID, Confirmed: true}
		if protocol == "trojan" || protocol == "hysteria2" {
			client.Password = h
		} else {
			client.UUID = h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
		}
		raw, _ := json.Marshal(client)
		sealed, err = secretstore.Seal(key, raw)
		if err != nil {
			return r, err
		}
	} else {
		var n int
		if e = s.Store.DB.QueryRowContext(ctx, `SELECT count(*) FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id WHERE c.id=? AND k.inbound_id=? AND c.status='available'`, d.CredentialID, d.InboundID).Scan(&n); e != nil || n != 1 {
			return r, fmt.Errorf("原生客户端凭据不属于所选入站或不可用")
		}
		if e = s.Store.DB.QueryRowContext(ctx, `SELECT core_client_ref FROM credential_material WHERE id=?`, d.CredentialID).Scan(&client.Reference); e != nil {
			return r, e
		}
		if e = s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_credentials WHERE credential_id=? AND enabled=1 AND subscriber_id!=?", d.CredentialID, d.Subscriber.ID).Scan(&n); e != nil {
			return r, e
		}
		if n > 0 {
			return r, fmt.Errorf("此原生客户端已归属其他订阅用户，请使用既有用户管理，不能重复纳管")
		}
	}
	// Keep a subscriber disabled until provisioning and credential binding succeed.
	wanted := d.Subscriber.Enabled
	existingID := d.Subscriber.ID
	if existingID != "" {
		if err := s.Store.DB.QueryRowContext(ctx, "SELECT enabled FROM subscribers WHERE id=?", existingID).Scan(&wanted); err != nil {
			return r, err
		}
	}
	d.Subscriber.Enabled = false
	var credential any
	if d.Mode == "native" {
		credential = d.CredentialID
	}
	insertOperation := func(tx *sql.Tx, id string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO subscriber_provisioning(request_id,request_hash,subscriber_id,inbound_id,client_ref,target_endpoint,target_tag,target_protocol,origin,credential_id,desired_sealed,status,requested_enabled,checked_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending',?,?)`, d.RequestID, fingerprint, id, d.InboundID, client.Reference, endpoint, tag, protocol, d.Mode, credential, sealed, wanted, storage.Stamp(time.Now()))
		return err
	}
	if existingID == "" {
		_, e = subscription.SaveSubscriberWith(ctx, s.Store, d.Subscriber, insertOperation)
	} else {
		tx, err := s.Store.DB.BeginTx(ctx, nil)
		if err != nil {
			return r, err
		}
		defer tx.Rollback()
		if e = insertOperation(tx, existingID); e == nil {
			e = tx.Commit()
		}
	}
	if e != nil {
		return r, e
	}
	return s.Retry(ctx, d.RequestID)
}

func (s Service) Retry(ctx context.Context, request string) (Result, error) {
	r := Result{RequestID: request}
	var inbound, ref, origin, sealed, status string
	var credential sql.NullString
	var enabled bool
	e := s.Store.DB.QueryRowContext(ctx, `SELECT subscriber_id,inbound_id,client_ref,origin,credential_id,desired_sealed,status,requested_enabled FROM subscriber_provisioning WHERE request_id=?`, request).Scan(&r.SubscriberID, &inbound, &ref, &origin, &credential, &sealed, &status, &enabled)
	if e != nil {
		return r, e
	}
	if status == "ready" {
		r.Status = "ready"
		r.Message = "客户端和订阅用户凭据已绑定"
		return r, nil
	}
	claim, e := s.Store.DB.ExecContext(ctx, `UPDATE subscriber_provisioning SET status='applying',checked_at=? WHERE request_id=? AND (status!='applying' OR checked_at<?)`, storage.Stamp(time.Now()), request, storage.Stamp(time.Now().Add(-time.Minute)))
	if e != nil {
		return r, e
	}
	n, _ := claim.RowsAffected()
	if n != 1 {
		return r, fmt.Errorf("此创建请求正在执行，请稍后重试原请求")
	}
	finish := func(state, message string) (Result, error) {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, err := s.Store.DB.ExecContext(c, "UPDATE subscriber_provisioning SET status=?,message=?,checked_at=? WHERE request_id=?", state, message, storage.Stamp(time.Now()), request)
		r.Status = state
		r.Message = message
		return r, err
	}
	if origin == "managed" {
		var unchanged int
		if err := s.Store.DB.QueryRowContext(ctx, `SELECT count(*) FROM subscriber_provisioning p JOIN catalog_inbounds b ON b.id=p.inbound_id JOIN instances x ON x.id=b.instance_id WHERE p.request_id=? AND p.target_endpoint=x.api_endpoint AND p.target_tag=b.tag AND p.target_protocol=b.protocol AND b.present=1 AND b.review=0 AND b.retired=0`, request).Scan(&unchanged); err != nil || unchanged != 1 {
			return finish("pending", "目标端点、入站身份或状态变化，禁止向新目标自动重试")
		}
		key, er := secretstore.Key(s.Config.ManagedKeyFile)
		if er != nil {
			return finish("pending", "Desired 密钥不可用，未向核心添加用户")
		}
		raw, er := secretstore.Open(key, sealed)
		if er != nil {
			return finish("pending", "Desired 解密失败，未向核心添加用户")
		}
		var client remote.Client
		if json.Unmarshal(raw, &client) != nil {
			return finish("pending", "Desired 格式无效")
		}
		var coreType string
		if er = s.Store.DB.QueryRowContext(ctx, `SELECT i.core_type FROM catalog_inbounds b JOIN instances i ON i.id=b.instance_id WHERE b.id=?`, inbound).Scan(&coreType); er != nil {
			return finish("pending", "目标核心不可用")
		}
		if coreType == "hysteria2" {
			id, err := remote.RegisterHTTPAuthClient(ctx, s.Store, client)
			if err != nil {
				return finish("pending", "HTTP 认证账户登记失败，请检查后端确认状态或凭据冲突")
			}
			if err = hysteriaauth.LinkAccount(ctx, s.Store, inbound, id, client.Password); err != nil {
				return finish("pending", "HTTP 认证账户绑定失败，请检查后端目标与修订")
			}
			credential = sql.NullString{String: id, Valid: true}
		} else {
			var revision int64
			var expectedEndpoint, expectedTag, expectedProtocol string
			if er = s.Store.DB.QueryRowContext(ctx, "SELECT target_endpoint,target_tag,target_protocol FROM subscriber_provisioning WHERE request_id=?", request).Scan(&expectedEndpoint, &expectedTag, &expectedProtocol); er != nil {
				return finish("pending", "目标证据不可用")
			}
			// An explicit retry may reclaim a stale local write lease. Mutate still
			// reads the actual user before adding and refuses conflicting credentials.
			if _, er = s.Store.DB.ExecContext(ctx, `UPDATE remote_runtime_users SET status='unknown' WHERE inbound_id=? AND client_ref=? AND status='applying' AND checked_at<? AND api_endpoint=? AND core_tag=? AND protocol=?`, inbound, ref, storage.Stamp(time.Now().Add(-time.Minute)), expectedEndpoint, expectedTag, expectedProtocol); er != nil {
				return finish("pending", "未能恢复中断的本地操作")
			}
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT revision FROM remote_runtime_users WHERE inbound_id=? AND client_ref=?", inbound, ref).Scan(&revision)
			result, er := remote.Mutate(ctx, s.Store, s.Config.ManagedKeyFile, s.Config.AllowedTargets, remote.Mutation{Client: client, Action: "add", Revision: revision, RestoreAuthorized: true, ExpectedEndpoint: expectedEndpoint, ExpectedTag: expectedTag, ExpectedProtocol: expectedProtocol})
			if er != nil {
				return finish("pending", "核心添加未完成；请检查 API 能力、连通性后重试原请求")
			}
			if result.Status != "runtime_verified" {
				return finish("pending", "核心尚未回读确认："+result.Status)
			}
			er = s.Store.DB.QueryRowContext(ctx, `SELECT credential_id FROM inbound_client_keys WHERE inbound_id=? AND client_ref=?`, inbound, ref).Scan(&credential)
			if er != nil {
				return finish("pending", "核心已回读，本地凭据绑定待重试")
			}
		}
	}
	if !credential.Valid {
		return finish("pending", "缺少真实客户端凭据")
	}
	if _, e = s.Store.DB.ExecContext(ctx, "UPDATE subscriber_provisioning SET credential_id=? WHERE request_id=?", credential.String, request); e != nil {
		return finish("pending", "凭据任务关联待重试")
	}
	if _, e = subscription.Bind(ctx, s.Store, subscription.Binding{SubscriberID: r.SubscriberID, InboundID: &inbound, CredentialID: credential.String, Enabled: true}); e != nil {
		return finish("pending", "客户端已存在，凭据授权未完成；可能已归属其他用户，请核查后重试")
	}
	// v2.1 node workflows activate only after every required target is ready.
	// Child retries must not re-enable an administrator-disabled subscriber.
	_, e = s.Store.DB.ExecContext(ctx, "UPDATE subscribers SET enabled=?,updated_at=? WHERE id=? AND NOT EXISTS(SELECT 1 FROM subscriber_node_tasks WHERE child_request_id=?)", enabled, storage.Stamp(time.Now()), r.SubscriberID, request)
	if e != nil {
		return finish("pending", "凭据已绑定，用户启用待重试")
	}
	var backend int
	_ = s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM hysteria_auth_accounts WHERE credential_id=? AND origin='managed'", credential.String).Scan(&backend)
	if backend == 1 {
		return finish("ready", "HTTP 认证数据库账户已就绪；核心配置由管理员确认，实际登录证据请查看后端状态；无需 AddUser 或重启恢复")
	}
	return finish("ready", "客户端已确认并绑定；TML 动态用户仅在验证缺失时恢复，原生用户未改写")
}
