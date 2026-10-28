package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"kept/server/internal/api/gen"
	"kept/server/internal/auth"
	"kept/server/internal/edition"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// deviceOperations are all an upload-only phone (App 1.x) may call (design
// doc 9.6); public operations are open to it anyway.
var deviceOperations = map[string]bool{
	"uploaddocument":      true,
	"getmobilebootstrap":  true,
	"getuploadstatus":     true,
	"forgetcurrentdevice": true,
}

// deviceDenied are what a full-access phone (App 2.0) may not call although
// its role allows: account security, pairing, workspace and member
// administration, and the instance's administration (every "admin…"
// operation, checked by prefix). A lost phone can be disconnected on the
// web; it cannot lock its owner out or widen its own access.
var deviceDenied = map[string]bool{
	"updateme": true, "setmylocale": true, "changepassword": true, "listtokens": true, "createtoken": true, "revoketoken": true,
	"gettwofactor": true, "begintwofactor": true, "enabletwofactor": true, "disabletwofactor": true, "regeneraterecoverycodes": true,
	"listdevices": true, "revokedevice": true, "createpairingcode": true,
	"createworkspace": true, "updateworkspace": true, "deleteworkspace": true, "movedocuments": true, "exportworkspace": true, // moving between spaces and exporting a whole space stay on the web
	"getstoragelocation": true, "setstoragelocation": true, "setstoragecold": true, "setkeepversions": true, "listmemberuploads": true, "setmemberuploadquota": true, "getremoteinbox": true, "setremoteinbox": true, "scanremoteinbox": true, // where a space keeps its files (v1.7.2): on the web
	"listmembers": true, "addmember": true, "updatemember": true, "removemember": true,
	"logout": true, "completesetup": true,
	// v1.6.4: confirming the password is the web's; emergency access is
	// about the account and the workspace's owners, not a phone's business
	"listemergencycontacts": true, "addemergencycontact": true, "updateemergencycontact": true, "removeemergencycontact": true,
	"approveemergencyaccess": true, "denyemergencyaccess": true, "revokeemergencyaccess": true,
	"listmyemergencyaccess": true, "requestemergencyaccess": true, "cancelemergencyaccess": true,
}

// deviceAllowed reports whether a phone with the given access may call op.
func deviceAllowed(access, op string) bool {
	if access != auth.AccessFull {
		return deviceOperations[op]
	}
	return !deviceDenied[op] && !strings.HasPrefix(op, "admin")
}

var errNotADevice = &apiError{http.StatusBadRequest, "not_a_device", "只有手机可以这样断开自己"}

func (s *Server) CreatePairingCode(ctx context.Context, _ gen.CreatePairingCodeRequestObject) (gen.CreatePairingCodeResponseObject, error) {
	p := mustPrincipal(ctx)
	if p.Via != auth.ViaSession {
		return nil, &apiError{http.StatusForbidden, "forbidden", "请在网页端生成配对码"}
	}
	scope := tenant.MustFrom(ctx)
	_, ws, err := s.workspaces.Resolve(ctx, p.UserID, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	pc, err := s.auth.CreatePairingCode(ctx, p.UserID, scope)
	if err != nil {
		return nil, err
	}
	urls := s.pairingURLs(ctx)
	qr, err := json.Marshal(map[string]any{"v": 1, "urls": urls, "code": pc.Code, "ws": ws.Name})
	if err != nil {
		return nil, err
	}
	return gen.CreatePairingCode201JSONResponse{
		Code: pc.Code, ExpiresAt: pc.ExpiresAt, WorkspaceName: ws.Name, Urls: urls, Qr: string(qr),
	}, nil
}

// pairingURLs lists the addresses a phone tries: KEPT_PUBLIC_URLS first,
// then the address the browser used to reach this server.
func (s *Server) pairingURLs(ctx context.Context) []string {
	urls := slices.Clone(s.publicURLs)
	if r, ok := ctx.Value(httpRequestKey{}).(*http.Request); ok && r.Host != "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		if own := scheme + "://" + r.Host; !slices.Contains(urls, own) {
			urls = append(urls, own)
		}
	}
	if urls == nil {
		urls = []string{}
	}
	return urls
}

func (s *Server) PairDevice(ctx context.Context, req gen.PairDeviceRequestObject) (gen.PairDeviceResponseObject, error) {
	name := ""
	if req.Body.DeviceName != nil {
		name = *req.Body.DeviceName
	}
	access := auth.AccessUpload
	if req.Body.Access != nil {
		access = string(*req.Body.Access)
	}
	d, err := s.auth.Pair(ctx, req.Body.Code, name, access, clientFrom(ctx).ClientInfo)
	if err != nil {
		return nil, err
	}
	return gen.PairDevice200JSONResponse{
		Token: d.Token, DeviceId: d.DeviceID, UserName: d.UserName, MaxUploadSize: s.ingest.MaxUploadSize(),
		Workspace: gen.WorkspaceRef{Id: d.WorkspaceID, Name: d.WorkspaceName}, Access: gen.DeviceAccess(d.Access),
	}, nil
}

func (s *Server) ListDevices(ctx context.Context, _ gen.ListDevicesRequestObject) (gen.ListDevicesResponseObject, error) {
	ds, err := s.auth.ListDevices(ctx, mustPrincipal(ctx).UserID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListDevices200JSONResponse, len(ds))
	for i, d := range ds {
		access := gen.DeviceAccess(d.Access)
		out[i] = gen.Device{Id: d.ID, Name: d.Name, WorkspaceId: d.WorkspaceID, WorkspaceName: d.WorkspaceName, PairedAt: d.PairedAt, LastSeenAt: d.LastSeenAt, Access: &access}
	}
	return out, nil
}

func (s *Server) RevokeDevice(ctx context.Context, req gen.RevokeDeviceRequestObject) (gen.RevokeDeviceResponseObject, error) {
	if err := s.auth.RevokeDevice(ctx, mustPrincipal(ctx).UserID, req.Id); err != nil {
		return nil, err
	}
	return gen.RevokeDevice204Response{}, nil
}

func (s *Server) ForgetCurrentDevice(ctx context.Context, _ gen.ForgetCurrentDeviceRequestObject) (gen.ForgetCurrentDeviceResponseObject, error) {
	p := mustPrincipal(ctx)
	if p.Via != auth.ViaDevice {
		return nil, errNotADevice
	}
	if err := s.auth.RevokeDevice(ctx, p.UserID, p.TokenID); err != nil {
		return nil, err
	}
	return gen.ForgetCurrentDevice204Response{}, nil
}

func (s *Server) GetMobileBootstrap(ctx context.Context, _ gen.GetMobileBootstrapRequestObject) (gen.GetMobileBootstrapResponseObject, error) {
	p := mustPrincipal(ctx)
	scope := tenant.MustFrom(ctx)
	_, ws, err := s.workspaces.Resolve(ctx, p.UserID, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := gen.GetMobileBootstrap200JSONResponse{
		Workspace: gen.WorkspaceRef{Id: ws.ID, Name: ws.Name}, UserName: p.DisplayName,
		MaxUploadSize: s.ingest.MaxUploadSize(), AcceptedTypes: s.ingest.AcceptedTypes(),
		Access: gen.DeviceAccess(auth.AccessUpload), Role: gen.WorkspaceRole(scope.Role()),
		Ai: s.qaAvailable(ctx, scope.WorkspaceID()) && s.editionAllows(edition.Pro),
	}
	// App 4.9 hides what the edition does not include (v1.12). Always sent:
	// App 4.10 takes a bootstrap without it for a server too old to use.
	ed, err := s.editionStatus(ctx)
	if err != nil {
		return nil, err
	}
	e := gen.MobileBootstrapEdition(ed.Edition)
	out.Edition = &e
	feats := make([]string, 0, len(ed.Features))
	for _, f := range ed.Features {
		feats = append(feats, string(f))
	}
	out.Features = &feats
	if p.Via == auth.ViaDevice {
		out.Access = gen.DeviceAccess(p.DeviceAccess)
	}
	drawers, err := s.drawers.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, d := range drawers {
		out.Drawers = append(out.Drawers, struct {
			Color string `json:"color"`
			Icon  string `json:"icon"`
			Id    int64  `json:"id"`
			Name  string `json:"name"`
		}{Color: d.Color, Icon: d.Icon, Id: d.ID, Name: d.Name})
	}
	tags, err := s.taxonomy.ListTags(ctx, scope)
	if err != nil {
		return nil, err
	}
	byID := map[int64]taxonomy.TagItem{}
	for _, t := range tags {
		byID[t.ID] = t
	}
	for _, t := range tags {
		path := t.Name
		for parent, depth := t.ParentID, 0; parent != 0 && depth < 16; depth++ {
			pt, ok := byID[parent]
			if !ok {
				break
			}
			path = pt.Name + " / " + path
			parent = pt.ParentID
		}
		out.Tags = append(out.Tags, struct {
			Color string  `json:"color"`
			Id    int64   `json:"id"`
			Name  string  `json:"name"`
			Path  *string `json:"path,omitempty"`
		}{Color: t.Color, Id: t.ID, Name: t.Name, Path: &path})
	}
	types, err := s.taxonomy.List(ctx, scope, taxonomy.DocumentType)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		out.DocumentTypes = append(out.DocumentTypes, struct {
			Id   int64  `json:"id"`
			Name string `json:"name"`
		}{Id: t.ID, Name: t.Name})
	}
	// an empty workspace: [] not null, which the phone cannot read (a nil
	// slice sliced is still nil)
	out.Drawers, out.Tags, out.DocumentTypes = nonNil(out.Drawers), nonNil(out.Tags), nonNil(out.DocumentTypes)
	return out, nil
}

func (s *Server) GetUploadStatus(ctx context.Context, req gen.GetUploadStatusRequestObject) (gen.GetUploadStatusResponseObject, error) {
	st, err := s.ingest.Statuses(ctx, tenant.MustFrom(ctx), req.Body.ClientUploadIds)
	if err != nil {
		return nil, err
	}
	out := make(gen.GetUploadStatus200JSONResponse, len(st))
	for i, u := range st {
		out[i] = gen.UploadStatus{ClientUploadId: u.ClientUploadID, Status: gen.UploadStatusStatus(u.Status)}
		if u.DocumentID != 0 {
			id := u.DocumentID
			out[i].DocumentId = &id
		}
		if u.Error != "" {
			e := u.Error
			out[i].Error = &e
		}
	}
	return out, nil
}
