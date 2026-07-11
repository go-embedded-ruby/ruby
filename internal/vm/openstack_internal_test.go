// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// This test drives the OpenStack binding (require "openstack") against an
// in-process fake OpenStack cloud served over net/http/httptest and reached
// through the per-VM transport seam (vm.osTransport) — so full coverage needs no
// live cloud and no real socket dependence beyond loopback. The mock is modelled
// on the go-ruby-openstack library's own mock_test.go: a Keystone v3 token whose
// service catalog points every service back at the same server, plus canned JSON
// for the core resources of the six wrapped services. statusOverride replaces
// every resource (not auth) response so the adapter's error branches are driven
// without bespoke servers.

// --- fake cloud -------------------------------------------------------------

type osMockCloud struct {
	*httptest.Server
	statusOverride int
	authStatus     int  // when non-zero, the auth exchange returns this status
	emptyCatalog   bool // when true, the issued token carries an empty catalog
}

const (
	osServerObj = `{"server":{"id":"srv-1","name":"web","status":"ACTIVE"}}`
	osServerLst = `{"servers":[{"id":"srv-1","name":"web","status":"ACTIVE"}]}`
	osFlavorObj = `{"flavor":{"id":"1","name":"m1.tiny","ram":512,"vcpus":1,"disk":1}}`
	osFlavorLst = `{"flavors":[{"id":"1","name":"m1.tiny","ram":512,"vcpus":1,"disk":1}]}`
	osKpObj     = `{"keypair":{"name":"kp","fingerprint":"aa:bb","public_key":"ssh-rsa AAAA"}}`
	osKpLst     = `{"keypairs":[{"keypair":{"name":"kp","public_key":"ssh-rsa AAAA"}}]}`
	osAttachObj = `{"volumeAttachment":{"id":"vol-1","volumeId":"vol-1","serverId":"srv-1","device":"/dev/vdb"}}`

	osVolObj  = `{"volume":{"id":"vol-1","name":"data","size":10,"status":"available"}}`
	osVolLst  = `{"volumes":[{"id":"vol-1","name":"data","size":10,"status":"available"}]}`
	osSnapObj = `{"snapshot":{"id":"snap-1","name":"snap","volume_id":"vol-1","size":10,"status":"available"}}`
	osSnapLst = `{"snapshots":[{"id":"snap-1","name":"snap","volume_id":"vol-1","size":10,"status":"available"}]}`
	osVtObj   = `{"volume_type":{"id":"vt-1","name":"lvm"}}`
	osVtLst   = `{"volume_types":[{"id":"vt-1","name":"lvm"}]}`
	osImgObj  = `{"id":"img-1","name":"cirros","status":"active","visibility":"public"}`
	osImgLst  = `{"images":[{"id":"img-1","name":"cirros","status":"active","visibility":"public"}]}`
	osNetObj  = `{"network":{"id":"net-1","name":"private","status":"ACTIVE","admin_state_up":true}}`
	osNetLst  = `{"networks":[{"id":"net-1","name":"private","status":"ACTIVE","admin_state_up":true}]}`
	osSubObj  = `{"subnet":{"id":"sub-1","name":"sub","network_id":"net-1","cidr":"10.0.0.0/24","ip_version":4}}`
	osSubLst  = `{"subnets":[{"id":"sub-1","name":"sub","network_id":"net-1","cidr":"10.0.0.0/24","ip_version":4}]}`
	osPortObj = `{"port":{"id":"port-1","name":"p","network_id":"net-1","admin_state_up":true}}`
	osPortLst = `{"ports":[{"id":"port-1","name":"p","network_id":"net-1","admin_state_up":true}]}`
	osRtrObj  = `{"router":{"id":"rtr-1","name":"r","admin_state_up":true,"status":"ACTIVE"}}`
	osRtrLst  = `{"routers":[{"id":"rtr-1","name":"r","admin_state_up":true,"status":"ACTIVE"}]}`
	osSgObj   = `{"security_group":{"id":"sg-1","name":"default"}}`
	osSgLst   = `{"security_groups":[{"id":"sg-1","name":"default"}]}`
	osSgrObj  = `{"security_group_rule":{"id":"sgr-1","direction":"ingress","security_group_id":"sg-1"}}`
	osSgrLst  = `{"security_group_rules":[{"id":"sgr-1","direction":"ingress","security_group_id":"sg-1"}]}`
	osFipObj  = `{"floatingip":{"id":"fip-1","floating_ip_address":"1.2.3.4","status":"ACTIVE"}}`
	osFipLst  = `{"floatingips":[{"id":"fip-1","floating_ip_address":"1.2.3.4","status":"ACTIVE"}]}`
	osProjObj = `{"project":{"id":"proj-1","name":"demo","domain_id":"default","enabled":true}}`
	osProjLst = `{"projects":[{"id":"proj-1","name":"demo","domain_id":"default","enabled":true}]}`
	osUsrObj  = `{"user":{"id":"user-1","name":"alice","domain_id":"default","enabled":true}}`
	osUsrLst  = `{"users":[{"id":"user-1","name":"alice","domain_id":"default","enabled":true}]}`
	osRoleObj = `{"role":{"id":"role-1","name":"admin"}}`
	osRoleLst = `{"roles":[{"id":"role-1","name":"admin"}]}`
	osDomObj  = `{"domain":{"id":"dom-1","name":"Default","enabled":true}}`
	osDomLst  = `{"domains":[{"id":"dom-1","name":"Default","enabled":true}]}`
	osContLst = `[{"name":"logs","count":2,"bytes":100}]`
	osObjLst  = `[{"name":"a.txt","bytes":11,"content_type":"text/plain","hash":"x","last_modified":"2026-01-01T00:00:00.000000"}]`
	osObjBody = "hello world"
)

func startOSMock(t *testing.T) *osMockCloud {
	t.Helper()
	mc := &osMockCloud{}
	mc.Server = httptest.NewServer(mc.handler())
	t.Cleanup(mc.Server.Close)
	return mc
}

func (mc *osMockCloud) write(w http.ResponseWriter, status int, body string) {
	if mc.statusOverride != 0 {
		status = mc.statusOverride
		body = `{"error":{"message":"forced"}}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func (mc *osMockCloud) pageList(r *http.Request, body string) string {
	if r.URL.Query().Get("marker") != "" && strings.HasPrefix(strings.TrimSpace(body), "[") {
		return "[]"
	}
	return body
}

func (mc *osMockCloud) collection(mux *http.ServeMux, path, listBody, objBody string, createStatus int) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mc.write(w, createStatus, objBody)
			return
		}
		mc.write(w, 200, mc.pageList(r, listBody))
	})
	mux.HandleFunc(path+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mc.write(w, 204, "")
			return
		}
		mc.write(w, 200, objBody)
	})
}

func (mc *osMockCloud) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /identity/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		if mc.authStatus != 0 {
			w.WriteHeader(mc.authStatus)
			return
		}
		w.Header().Set("X-Subject-Token", "faketoken")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, osCatalog("http://"+r.Host, mc.emptyCatalog))
	})

	versions := func(id string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{"versions":[{"id":"`+id+`","status":"CURRENT"}]}`)
		}
	}
	mux.HandleFunc("GET /compute/{$}", versions("v2.1"))
	mux.HandleFunc("GET /network/{$}", versions("v2.0"))
	mux.HandleFunc("GET /volume/{$}", versions("v3.0"))
	mux.HandleFunc("GET /image/{$}", versions("v2.0"))

	// Compute (Nova).
	mux.HandleFunc("GET /compute/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		mc.write(w, 200, mc.pageList(r, osServerLst))
	})
	mux.HandleFunc("POST /compute/servers", func(w http.ResponseWriter, _ *http.Request) { mc.write(w, 202, osServerObj) })
	mux.HandleFunc("/compute/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mc.write(w, 204, "")
			return
		}
		mc.write(w, 200, osServerObj)
	})
	mux.HandleFunc("POST /compute/servers/{id}/action", func(w http.ResponseWriter, _ *http.Request) { mc.write(w, 202, "") })
	mux.HandleFunc("POST /compute/servers/{id}/os-volume_attachments", func(w http.ResponseWriter, _ *http.Request) {
		mc.write(w, 200, osAttachObj)
	})
	mux.HandleFunc("DELETE /compute/servers/{id}/os-volume_attachments/{aid}", func(w http.ResponseWriter, _ *http.Request) {
		mc.write(w, 204, "")
	})
	mux.HandleFunc("GET /compute/flavors/detail", func(w http.ResponseWriter, r *http.Request) {
		mc.write(w, 200, mc.pageList(r, osFlavorLst))
	})
	mux.HandleFunc("GET /compute/flavors/{id}", func(w http.ResponseWriter, _ *http.Request) { mc.write(w, 200, osFlavorObj) })
	mux.HandleFunc("/compute/os-keypairs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mc.write(w, 201, osKpObj)
			return
		}
		mc.write(w, 200, mc.pageList(r, osKpLst))
	})
	mux.HandleFunc("/compute/os-keypairs/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mc.write(w, 204, "")
			return
		}
		mc.write(w, 200, osKpObj)
	})

	// Block storage (Cinder).
	mux.HandleFunc("GET /volume/volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		mc.write(w, 200, mc.pageList(r, osVolLst))
	})
	mux.HandleFunc("POST /volume/volumes", func(w http.ResponseWriter, _ *http.Request) { mc.write(w, 202, osVolObj) })
	mux.HandleFunc("/volume/volumes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mc.write(w, 204, "")
			return
		}
		mc.write(w, 200, osVolObj)
	})
	mux.HandleFunc("GET /volume/snapshots/detail", func(w http.ResponseWriter, r *http.Request) {
		mc.write(w, 200, mc.pageList(r, osSnapLst))
	})
	mux.HandleFunc("POST /volume/snapshots", func(w http.ResponseWriter, _ *http.Request) { mc.write(w, 202, osSnapObj) })
	mux.HandleFunc("/volume/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mc.write(w, 204, "")
			return
		}
		mc.write(w, 200, osSnapObj)
	})
	mc.collection(mux, "/volume/types", osVtLst, osVtObj, 200)

	// Image (Glance).
	mux.HandleFunc("/image/v2/images", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mc.write(w, 201, osImgObj)
			return
		}
		mc.write(w, 200, osImgLst)
	})
	mux.HandleFunc("/image/v2/images/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete, http.MethodPut:
			mc.write(w, 204, "")
		default:
			mc.write(w, 200, osImgObj)
		}
	})
	mux.HandleFunc("PUT /image/v2/images/{id}/file", func(w http.ResponseWriter, _ *http.Request) { mc.write(w, 204, "") })

	// Network (Neutron).
	mc.collection(mux, "/network/v2.0/networks", osNetLst, osNetObj, 201)
	mc.collection(mux, "/network/v2.0/subnets", osSubLst, osSubObj, 201)
	mc.collection(mux, "/network/v2.0/ports", osPortLst, osPortObj, 201)
	mc.collection(mux, "/network/v2.0/routers", osRtrLst, osRtrObj, 201)
	mc.collection(mux, "/network/v2.0/security-groups", osSgLst, osSgObj, 201)
	mc.collection(mux, "/network/v2.0/security-group-rules", osSgrLst, osSgrObj, 201)
	mc.collection(mux, "/network/v2.0/floatingips", osFipLst, osFipObj, 201)

	// Identity (Keystone).
	mc.collection(mux, "/identity/v3/projects", osProjLst, osProjObj, 201)
	mc.collection(mux, "/identity/v3/users", osUsrLst, osUsrObj, 201)
	mc.collection(mux, "/identity/v3/roles", osRoleLst, osRoleObj, 201)
	mc.collection(mux, "/identity/v3/domains", osDomLst, osDomObj, 201)

	// Object storage (Swift).
	mux.HandleFunc("GET /object/{$}", func(w http.ResponseWriter, r *http.Request) { mc.write(w, 200, mc.pageList(r, osContLst)) })
	mux.HandleFunc("/object/{container}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			mc.write(w, 201, "")
		case http.MethodDelete:
			mc.write(w, 204, "")
		default:
			mc.write(w, 200, mc.pageList(r, osObjLst))
		}
	})
	mux.HandleFunc("/object/{container}/{object}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			mc.write(w, 201, "")
		case http.MethodDelete:
			mc.write(w, 204, "")
		default:
			mc.write(w, 200, osObjBody)
		}
	})

	return mux
}

func osCatalog(base string, empty bool) string {
	entry := func(typ, url string) string {
		return `{"type":"` + typ + `","name":"` + typ + `","endpoints":[` +
			`{"id":"1","interface":"public","region":"RegionOne","region_id":"RegionOne","url":"` + url + `"}]}`
	}
	entries := ""
	if !empty {
		entries = strings.Join([]string{
			entry("identity", base+"/identity/v3/"),
			entry("compute", base+"/compute/"),
			entry("network", base+"/network/"),
			entry("block-storage", base+"/volume/"),
			entry("object-store", base+"/object/"),
			entry("image", base+"/image/"),
		}, ",")
	}
	return `{"token":{"methods":["password"],"expires_at":"2035-01-01T00:00:00.000000Z",` +
		`"user":{"id":"user-1","name":"admin","domain":{"id":"default","name":"Default"}},` +
		`"project":{"id":"proj-1","name":"demo","domain":{"id":"default","name":"Default"}},` +
		`"catalog":[` + entries + `]}}`
}

// --- harness ----------------------------------------------------------------

// osRun runs a Ruby program (with `require "openstack"` prepended) on a fresh VM
// whose OpenStack HTTP transport is injected to reach the fake cloud mc, and with
// the cloud's base URL bound to the MOCK_URL constant. When inject is false the
// transport seam is left nil, so connect uses gophercloud's default transport
// (still reaching the loopback mock) — covering both sides of the seam.
func osRun(t *testing.T, mc *osMockCloud, inject bool, body string) string {
	t.Helper()
	prog, err := parser.Parse("require \"openstack\"\n" + body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	vm := New(&buf)
	if inject {
		vm.osTransport = mc.Client().Transport
	}
	vm.SetConst("MOCK_URL", object.NewString(mc.URL))
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// osConnectExpr is the OpenStack.connect(...) call every test opens with.
const osConnectExpr = `conn = OpenStack.connect(auth_url: MOCK_URL + "/identity/v3", ` +
	`username: "admin", password: "secret", project_name: "demo", domain_name: "Default", ` +
	`region: "RegionOne", allow_reauth: true)`

// osConnectPlain omits allow_reauth so a forced 401 on a resource call surfaces
// as a plain 401 (mapped to OpenStack::AuthError) rather than triggering
// gophercloud's transparent re-authentication retry.
const osConnectPlain = `conn = OpenStack.connect(auth_url: MOCK_URL + "/identity/v3", ` +
	`username: "admin", password: "secret", project_name: "demo", domain_name: "Default", region: "RegionOne")`

// TestOpenStackComputeAndConnection exercises the whole compute surface plus the
// connection accessors through the injected transport, asserting the Ruby Hash /
// Array shape of representative results and that every server action / keypair /
// volume-attachment call round-trips.
func TestOpenStackComputeAndConnection(t *testing.T) {
	mc := startOSMock(t)
	got := osRun(t, mc, true, osConnectExpr+`
c = conn.compute
puts c.servers.inspect
puts c.server("srv-1").inspect
puts c.create_server({"name" => "web", "flavorRef" => "1", "imageRef" => "img-1"}).inspect
puts c.update_server("srv-1", {"name" => "web2"})["id"]
c.delete_server("srv-1")
puts c.flavors.length
puts c.flavor("1")["name"]
puts c.keypairs.length
puts c.keypair("kp")["name"]
puts c.create_keypair({"name" => "kp"})["name"]
c.delete_keypair("kp")
c.start_server("srv-1")
c.stop_server("srv-1")
c.reboot_server("srv-1")
c.reboot_server("srv-1", "HARD")
puts c.attach_volume("srv-1", {"volumeId" => "vol-1"})["id"]
c.detach_volume("srv-1", "vol-1")
puts "done"
`)
	want := strings.Join([]string{
		`[{"id" => "srv-1", "name" => "web", "status" => "ACTIVE"}]`,
		`{"id" => "srv-1", "name" => "web", "status" => "ACTIVE"}`,
		`{"id" => "srv-1", "name" => "web", "status" => "ACTIVE"}`,
		"srv-1", "1", "m1.tiny", "1", "kp", "kp", "vol-1", "done",
	}, "\n")
	if got != want {
		t.Fatalf("compute:\n got=%q\nwant=%q", got, want)
	}
}

// TestOpenStackNetwork exercises the Neutron surface (networks/subnets/ports/
// routers/security groups + rules/floating IPs) and their get/create/update/
// delete, asserting the boolean + string conversion on a representative network.
func TestOpenStackNetwork(t *testing.T) {
	mc := startOSMock(t)
	got := osRun(t, mc, true, osConnectExpr+`
n = conn.network
puts n.networks.inspect
puts n.get_network("net-1")["name"]
puts n.create_network({"name" => "private"})["id"]
puts n.update_network("net-1", {"name" => "p2"})["id"]
n.delete_network("net-1")
puts n.subnets.length
n.get_subnet("sub-1"); n.create_subnet({"network_id" => "net-1", "cidr" => "10.0.0.0/24", "ip_version" => 4}); n.update_subnet("sub-1", {"name" => "s2"}); n.delete_subnet("sub-1")
puts n.ports.length
n.get_port("port-1"); n.create_port({"network_id" => "net-1"}); n.update_port("port-1", {"name" => "p2"}); n.delete_port("port-1")
puts n.routers.length
n.get_router("rtr-1"); n.create_router({"name" => "r"}); n.update_router("rtr-1", {"name" => "r2"}); n.delete_router("rtr-1")
puts n.security_groups.length
n.get_security_group("sg-1"); n.create_security_group({"name" => "default"}); n.update_security_group("sg-1", {"name" => "d2"}); n.delete_security_group("sg-1")
puts n.security_group_rules.length
n.get_security_group_rule("sgr-1"); n.create_security_group_rule({"direction" => "ingress", "ethertype" => "IPv4", "security_group_id" => "sg-1"}); n.delete_security_group_rule("sgr-1")
puts n.floating_ips.length
n.get_floating_ip("fip-1"); n.create_floating_ip({"floating_network_id" => "net-1"}); n.update_floating_ip("fip-1", {"port_id" => "port-1"}); n.delete_floating_ip("fip-1")
puts "done"
`)
	want := strings.Join([]string{
		`[{"admin_state_up" => true, "id" => "net-1", "name" => "private", "status" => "ACTIVE"}]`,
		"private", "net-1", "net-1", "1", "1", "1", "1", "1", "1", "done",
	}, "\n")
	if got != want {
		t.Fatalf("network:\n got=%q\nwant=%q", got, want)
	}
}

// TestOpenStackBlockStorageImageIdentity exercises Cinder, Glance and Keystone
// CRUD plus Swift containers/objects, including the byte-body get_object and the
// upload_image / put_object streaming paths.
func TestOpenStackBlockStorageImageIdentity(t *testing.T) {
	mc := startOSMock(t)
	got := osRun(t, mc, true, osConnectExpr+`
b = conn.block_storage
puts b.volumes.length
b.get_volume("vol-1"); b.create_volume({"size" => 10, "name" => "data"}); b.update_volume("vol-1", {"name" => "d2"}); b.delete_volume("vol-1")
puts b.snapshots.length
b.get_snapshot("snap-1"); b.create_snapshot({"volume_id" => "vol-1", "name" => "snap"}); b.update_snapshot("snap-1", {"name" => "s2"}); b.delete_snapshot("snap-1")
puts b.volume_types.length
b.get_volume_type("vt-1"); b.create_volume_type({"name" => "lvm"}); b.update_volume_type("vt-1", {"name" => "lvm2"}); b.delete_volume_type("vt-1")
img = conn.image
puts img.images.length
puts img.get_image("img-1")["name"]
puts img.create_image({"name" => "cirros"})["id"]
img.upload_image("img-1", "rawdata")
img.delete_image("img-1")
o = conn.object_storage
puts o.containers.length
o.create_container("logs")
puts o.objects("logs").length
puts o.get_object("logs", "a.txt")
o.put_object("logs", "a.txt", "hello world")
o.delete_object("logs", "a.txt")
o.delete_container("logs")
id = conn.identity
puts id.projects.length
id.get_project("proj-1"); id.create_project({"name" => "demo"}); id.update_project("proj-1", {"description" => "d"}); id.delete_project("proj-1")
id.users; id.get_user("user-1"); id.create_user({"name" => "alice"}); id.update_user("user-1", {"email" => "a@b.c"}); id.delete_user("user-1")
id.roles; id.get_role("role-1"); id.create_role({"name" => "admin"}); id.update_role("role-1", {"description" => "d"}); id.delete_role("role-1")
id.domains; id.get_domain("dom-1"); id.create_domain({"name" => "D"}); id.update_domain("dom-1", {"description" => "d"}); id.delete_domain("dom-1")
puts "done"
`)
	want := strings.Join([]string{
		"1", "1", "1", "1", "cirros", "img-1", "1", "1", "hello world", "1", "done",
	}, "\n")
	if got != want {
		t.Fatalf("cinder/glance/keystone:\n got=%q\nwant=%q", got, want)
	}
}

// TestOpenStackTransportSeamDefault covers the osTransport == nil side of the
// injection seam: with no transport injected, connect uses gophercloud's default
// transport, still reaching the loopback mock.
func TestOpenStackTransportSeamDefault(t *testing.T) {
	mc := startOSMock(t)
	got := osRun(t, mc, false, osConnectExpr+`
puts conn.identity.projects.first["name"]
`)
	if got != "demo" {
		t.Fatalf("default transport: got=%q, want %q", got, "demo")
	}
}

// TestOpenStackErrorMapping drives every branch of osRaise: an HTTP status of
// 404/401/403/409/400/500 on a resource call maps onto the matching
// OpenStack::Error subclass (all of which rescue as OpenStack::Error), and each
// helper's error branch (list / get / delete / get_object) re-raises.
func TestOpenStackErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		class  string
	}{
		{404, "OpenStack::NotFound"},
		{401, "OpenStack::AuthError"},
		{403, "OpenStack::Forbidden"},
		{409, "OpenStack::Conflict"},
		{400, "OpenStack::BadRequest"},
		{500, "OpenStack::Error"},
	}
	for _, tc := range cases {
		mc := startOSMock(t)
		mc.statusOverride = tc.status
		got := osRun(t, mc, true, osConnectPlain+`
begin
  conn.compute.server("srv-1")
rescue OpenStack::Error => e
  puts e.class
  puts(e.is_a?(StandardError))
end
`)
		want := tc.class + "\ntrue"
		if got != want {
			t.Fatalf("status %d:\n got=%q\nwant=%q", tc.status, got, want)
		}
	}
}

// TestOpenStackErrorBranches covers the remaining result-helper error branches
// (osResources / osVoid / osBytes) under a forced 404, and the service-accessor
// error branch (osService) under an empty service catalog.
func TestOpenStackErrorBranches(t *testing.T) {
	mc := startOSMock(t)
	mc.statusOverride = 404
	got := osRun(t, mc, true, osConnectExpr+`
puts(begin; conn.compute.servers; rescue OpenStack::NotFound; "list-nf"; end)
puts(begin; conn.compute.delete_server("srv-1"); rescue OpenStack::NotFound; "del-nf"; end)
puts(begin; conn.object_storage.get_object("logs", "a.txt"); rescue OpenStack::NotFound; "obj-nf"; end)
`)
	if got != "list-nf\ndel-nf\nobj-nf" {
		t.Fatalf("error branches: got=%q", got)
	}

	empty := startOSMock(t)
	empty.emptyCatalog = true
	got = osRun(t, empty, true, osConnectExpr+`
puts(begin; conn.compute; rescue OpenStack::Error => e; "svc:" + e.class.to_s; end)
`)
	if got != "svc:OpenStack::Error" {
		t.Fatalf("service error: got=%q", got)
	}
}

// TestOpenStackConnectErrors covers the connect failure paths: a bad auth status
// raises OpenStack::AuthError, a missing auth_url (no keyword hash, and a
// non-Hash argument) raises OpenStack::AuthError from the library's guard.
func TestOpenStackConnectErrors(t *testing.T) {
	mc := startOSMock(t)
	mc.authStatus = 401
	got := osRun(t, mc, true, `
begin
  OpenStack.connect(auth_url: MOCK_URL + "/identity/v3", username: "x", password: "y", project_name: "demo", domain_name: "Default")
rescue OpenStack::AuthError => e
  puts(e.is_a?(OpenStack::Error))
end
begin; OpenStack.connect; rescue OpenStack::AuthError; puts "no-args"; end
begin; OpenStack.connect(42); rescue OpenStack::AuthError; puts "non-hash"; end
`)
	if got != "true\nno-args\nnon-hash" {
		t.Fatalf("connect errors: got=%q", got)
	}
}

// TestOpenStackArgGuards covers the argument guards on the wrapper methods: a
// missing id (ArgumentError), a missing attrs Hash (ArgumentError) and a non-Hash
// attrs (TypeError).
func TestOpenStackArgGuards(t *testing.T) {
	mc := startOSMock(t)
	got := osRun(t, mc, true, osConnectExpr+`
c = conn.compute
puts(begin; c.server; rescue ArgumentError; "id-arg"; end)
puts(begin; c.create_server; rescue ArgumentError; "attrs-arg"; end)
puts(begin; c.create_server(42); rescue TypeError; "attrs-type"; end)
`)
	if got != "id-arg\nattrs-arg\nattrs-type" {
		t.Fatalf("arg guards: got=%q", got)
	}
}

// TestOpenStackHelpers covers the pure Go seams directly: osOptGet reports false
// on a nil Hash (the defensive branch osConnectHash never triggers).
func TestOpenStackHelpers(t *testing.T) {
	if _, ok := osOptGet(nil, "auth_url"); ok {
		t.Fatalf("osOptGet(nil) should report false")
	}
}

// TestOpenStackWrapperMarkers covers the value-interface markers (ToS / Inspect /
// Truthy) of each wrapper type directly — they report a stable inspect string and
// are always truthy, and are never routed through Ruby dispatch (which uses
// classOf), so a direct call is the only way to exercise them.
func TestOpenStackWrapperMarkers(t *testing.T) {
	markers := []struct {
		v    object.Value
		want string
	}{
		{&OpenStackConnection{}, "#<OpenStack::Connection>"},
		{&OpenStackCompute{}, "#<OpenStack::Compute>"},
		{&OpenStackNetwork{}, "#<OpenStack::Network>"},
		{&OpenStackBlockStorage{}, "#<OpenStack::BlockStorage>"},
		{&OpenStackObjectStorage{}, "#<OpenStack::ObjectStorage>"},
		{&OpenStackImage{}, "#<OpenStack::Image>"},
		{&OpenStackIdentity{}, "#<OpenStack::Identity>"},
	}
	for _, m := range markers {
		if m.v.ToS() != m.want || m.v.Inspect() != m.want || !m.v.Truthy() {
			t.Fatalf("marker %s: ToS=%q Inspect=%q Truthy=%v", m.want, m.v.ToS(), m.v.Inspect(), m.v.Truthy())
		}
	}
}
