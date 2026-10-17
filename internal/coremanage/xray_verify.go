package coremanage

import (
	"context"
	"errors"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
	"traffic-manager-lite/internal/inboundconfig"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
)

func accountCredential(b []byte) string {
	for len(b) > 0 {
		field, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return ""
		}
		b = b[n:]
		if field == 1 && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return ""
			}
			return string(v)
		}
		n = protowire.ConsumeFieldValue(field, typ, b)
		if n < 0 {
			return ""
		}
		b = b[n:]
	}
	return ""
}
func verifyXrayUsers(ctx context.Context, conn *grpc.ClientConn, t Target) error {
	b, e := read(t.Path)
	if e != nil {
		return e
	}
	expected, e := inboundconfig.Parse("xray", b)
	if e != nil {
		return e
	}
	if len(expected) > 10000 {
		return errors.New("runtime verification user bound exceeded")
	}
	client := hp.NewHandlerServiceClient(conn)
	byTag := map[string][]*hp.User{}
	for _, u := range expected {
		users, ok := byTag[u.Tag]
		if !ok {
			response, e := client.GetInboundUsers(ctx, &hp.GetInboundUserRequest{Tag: u.Tag})
			if e != nil {
				return errors.New("Xray Handler enumeration unavailable; runtime users not verified")
			}
			users = response.Users
			byTag[u.Tag] = users
		}
		credential := u.Profile.UUID
		if credential == "" {
			credential = u.Profile.Password
		}
		found := false
		for _, r := range users {
			if r.Account != nil && accountCredential(r.Account.Value) == credential && (r.Email == u.Profile.UserKey || u.Profile.UserKey == credential) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("Xray configured user missing or authentication drifted")
		}
	}
	return nil
}
