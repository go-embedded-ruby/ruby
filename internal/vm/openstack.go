// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	openstack "github.com/go-ruby-openstack/openstack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the OpenStack module and its Ruby-facing API (require
// "openstack"). The whole OpenStack client — Keystone v3 authentication, the
// service catalog, request signing, pagination and the per-service HTTP APIs —
// lives in github.com/go-ruby-openstack/openstack, a pure-Go (CGO=0) Ruby-facing
// wrapper over the reference SDK github.com/gophercloud/gophercloud (imported,
// never vendored). rbgo owns only the object-model bridge: the Ruby OpenStack
// surface and the Ruby⇄Go value conversion (see openstack_bind.go). The library
// returns each resource as a Resource (map[string]any keyed by the wire
// snake_case JSON names), the natural shape for a Ruby Hash.
//
// Surface (a clean wrapper mirroring the library's method names one-to-one):
//
//	OpenStack.connect(auth_url:, username:, password:, project_name:,
//	                  domain_name:, region:, ...) -> OpenStack::Connection
//	conn.compute / .network / .block_storage / .object_storage / .image /
//	                  .identity -> the six service accessors
//	compute.servers / .server(id) / .create_server(attrs) /
//	                  .update_server(id, attrs) / .delete_server(id),
//	                  .flavors / .flavor(id), .keypairs / .keypair(name) /
//	                  .create_keypair(attrs) / .delete_keypair(name),
//	                  .start_server(id) / .stop_server(id) /
//	                  .reboot_server(id[, type]) /
//	                  .attach_volume(server_id, attrs) /
//	                  .detach_volume(server_id, volume_id)
//	network.networks / .subnets / .ports / .routers / .security_groups /
//	                  .security_group_rules / .floating_ips, each with
//	                  get_/create_/update_/delete_
//	block_storage.volumes / .snapshots / .volume_types, each with
//	                  get_/create_/update_/delete_
//	object_storage.containers / .create_container(name) /
//	                  .delete_container(name) / .objects(container) /
//	                  .get_object(c, n) / .put_object(c, n, data) /
//	                  .delete_object(c, n)
//	image.images / .get_image(id) / .create_image(attrs) /
//	                  .delete_image(id) / .upload_image(id, data)
//	identity.projects / .users / .roles / .domains, each with
//	                  get_/create_/update_/delete_
//
// Every resource CRUD call maps snake_case one-to-one onto the library's Go
// method of the corresponding name, so the coverage is exactly the CRUD the
// library exposes. The two library methods that do NOT take a Resource are not
// wrapped: Image.UpdateImage takes a typed images.UpdateOpts (a JSON-patch list,
// not an attribute hash) rather than a Resource, so it has no idiomatic Hash
// surface; less-common services and advanced per-resource operations remain
// reachable through gophercloud directly. See the library README for the full
// coverage matrix.
//
// The typed error tree (Error / NotFoundError / AuthError / ForbiddenError /
// ConflictError / BadRequestError) is mapped onto OpenStack::Error < StandardError
// and its five subclasses (see registerOpenStackErrors and osRaise).

// OpenStackConnection wraps a *openstack.Connection as a Ruby OpenStack::Connection.
// The authenticated session (token + service catalog) lives in the library; this
// shell only reports the Ruby class and hands out the service accessors.
type OpenStackConnection struct{ c *openstack.Connection }

func (o *OpenStackConnection) ToS() string     { return "#<OpenStack::Connection>" }
func (o *OpenStackConnection) Inspect() string { return o.ToS() }
func (o *OpenStackConnection) Truthy() bool    { return true }

// OpenStackCompute wraps a *openstack.Compute as a Ruby OpenStack::Compute.
type OpenStackCompute struct{ s *openstack.Compute }

func (o *OpenStackCompute) ToS() string     { return "#<OpenStack::Compute>" }
func (o *OpenStackCompute) Inspect() string { return o.ToS() }
func (o *OpenStackCompute) Truthy() bool    { return true }

// OpenStackNetwork wraps a *openstack.Network as a Ruby OpenStack::Network.
type OpenStackNetwork struct{ s *openstack.Network }

func (o *OpenStackNetwork) ToS() string     { return "#<OpenStack::Network>" }
func (o *OpenStackNetwork) Inspect() string { return o.ToS() }
func (o *OpenStackNetwork) Truthy() bool    { return true }

// OpenStackBlockStorage wraps a *openstack.BlockStorage as OpenStack::BlockStorage.
type OpenStackBlockStorage struct{ s *openstack.BlockStorage }

func (o *OpenStackBlockStorage) ToS() string     { return "#<OpenStack::BlockStorage>" }
func (o *OpenStackBlockStorage) Inspect() string { return o.ToS() }
func (o *OpenStackBlockStorage) Truthy() bool    { return true }

// OpenStackObjectStorage wraps a *openstack.ObjectStorage as
// OpenStack::ObjectStorage.
type OpenStackObjectStorage struct{ s *openstack.ObjectStorage }

func (o *OpenStackObjectStorage) ToS() string     { return "#<OpenStack::ObjectStorage>" }
func (o *OpenStackObjectStorage) Inspect() string { return o.ToS() }
func (o *OpenStackObjectStorage) Truthy() bool    { return true }

// OpenStackImage wraps a *openstack.Image as a Ruby OpenStack::Image.
type OpenStackImage struct{ s *openstack.Image }

func (o *OpenStackImage) ToS() string     { return "#<OpenStack::Image>" }
func (o *OpenStackImage) Inspect() string { return o.ToS() }
func (o *OpenStackImage) Truthy() bool    { return true }

// OpenStackIdentity wraps a *openstack.Identity as a Ruby OpenStack::Identity.
type OpenStackIdentity struct{ s *openstack.Identity }

func (o *OpenStackIdentity) ToS() string     { return "#<OpenStack::Identity>" }
func (o *OpenStackIdentity) Inspect() string { return o.ToS() }
func (o *OpenStackIdentity) Truthy() bool    { return true }

// registerOpenStack installs the OpenStack module (require "openstack"):
// OpenStack.connect and the OpenStack::Connection / service-accessor classes,
// plus the OpenStack::Error tree.
func (vm *VM) registerOpenStack() {
	mod := newClass("OpenStack", nil)
	mod.isModule = true
	vm.consts["OpenStack"] = mod
	vm.registerOpenStackErrors(mod)

	reg := func(simple string) *RClass {
		c := newClass("OpenStack::"+simple, vm.cObject)
		mod.consts[simple] = c
		vm.consts["OpenStack::"+simple] = c
		return c
	}
	connCls := reg("Connection")
	computeCls := reg("Compute")
	networkCls := reg("Network")
	blockCls := reg("BlockStorage")
	objectCls := reg("ObjectStorage")
	imageCls := reg("Image")
	identityCls := reg("Identity")

	// OpenStack.connect(auth_url:, username:, password:, ...) authenticates against
	// Keystone v3 and returns an OpenStack::Connection. An authentication failure
	// (or a missing auth_url) raises OpenStack::AuthError.
	mod.smethods["connect"] = &Method{name: "connect", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osConnect(args)
	}}

	vm.registerOpenStackConnection(connCls)
	vm.registerOpenStackCompute(computeCls)
	vm.registerOpenStackNetwork(networkCls)
	vm.registerOpenStackBlockStorage(blockCls)
	vm.registerOpenStackObjectStorage(objectCls)
	vm.registerOpenStackImage(imageCls)
	vm.registerOpenStackIdentity(identityCls)
}

// registerOpenStackErrors installs OpenStack::Error < StandardError and its five
// subclasses (NotFound / AuthError / Forbidden / Conflict / BadRequest), each
// registered both as a nested constant of OpenStack and under its qualified name
// in the top-level table so a raised error rescues as the right Ruby class.
func (vm *VM) registerOpenStackErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		c := newClass("OpenStack::"+simple, super)
		mod.consts[simple] = c
		vm.consts["OpenStack::"+simple] = c
		return c
	}
	base := reg("Error", std)
	reg("NotFound", base)
	reg("AuthError", base)
	reg("Forbidden", base)
	reg("Conflict", base)
	reg("BadRequest", base)
}

// registerOpenStackConnection installs the OpenStack::Connection service
// accessors (compute / network / block_storage / object_storage / image /
// identity). Each builds its service client through the library; a catalog or
// endpoint failure raises the mapped OpenStack::Error.
func (vm *VM) registerOpenStackConnection(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	d("compute", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self.(*OpenStackConnection).c.Compute()
		return vm.osService(&OpenStackCompute{s}, err)
	})
	d("network", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self.(*OpenStackConnection).c.Network()
		return vm.osService(&OpenStackNetwork{s}, err)
	})
	d("block_storage", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self.(*OpenStackConnection).c.BlockStorage()
		return vm.osService(&OpenStackBlockStorage{s}, err)
	})
	d("object_storage", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self.(*OpenStackConnection).c.ObjectStorage()
		return vm.osService(&OpenStackObjectStorage{s}, err)
	})
	d("image", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self.(*OpenStackConnection).c.Image()
		return vm.osService(&OpenStackImage{s}, err)
	})
	d("identity", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s, err := self.(*OpenStackConnection).c.Identity()
		return vm.osService(&OpenStackIdentity{s}, err)
	})
}

// registerOpenStackCompute installs the Nova (compute) surface: servers, flavors
// and keypairs CRUD plus the server power actions and volume attachment.
func (vm *VM) registerOpenStackCompute(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	c := func(self object.Value) *openstack.Compute { return self.(*OpenStackCompute).s }

	d("servers", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(c(self).Servers())
	})
	d("server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).Server(osArgStr(args, 0)))
	})
	d("create_server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).CreateServer(osArgAttrs(args, 0)))
	})
	d("update_server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).UpdateServer(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(c(self).DeleteServer(osArgStr(args, 0)))
	})
	d("flavors", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(c(self).Flavors())
	})
	d("flavor", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).Flavor(osArgStr(args, 0)))
	})
	d("keypairs", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(c(self).Keypairs())
	})
	d("keypair", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).Keypair(osArgStr(args, 0)))
	})
	d("create_keypair", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).CreateKeypair(osArgAttrs(args, 0)))
	})
	d("delete_keypair", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(c(self).DeleteKeypair(osArgStr(args, 0)))
	})
	d("start_server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(c(self).StartServer(osArgStr(args, 0)))
	})
	d("stop_server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(c(self).StopServer(osArgStr(args, 0)))
	})
	d("reboot_server", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		id := osArgStr(args, 0)
		method := "SOFT"
		if len(args) > 1 {
			method = strArg(args[1])
		}
		return vm.osVoid(c(self).RebootServer(id, method))
	})
	d("attach_volume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(c(self).AttachVolume(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("detach_volume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(c(self).DetachVolume(osArgStr(args, 0), osArgStr(args, 1)))
	})
}

// registerOpenStackNetwork installs the Neutron (network) surface: networks,
// subnets, ports, routers, security groups and their rules, and floating IPs.
func (vm *VM) registerOpenStackNetwork(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	n := func(self object.Value) *openstack.Network { return self.(*OpenStackNetwork).s }

	d("networks", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).Networks())
	})
	d("get_network", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetNetwork(osArgStr(args, 0)))
	})
	d("create_network", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreateNetwork(osArgAttrs(args, 0)))
	})
	d("update_network", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).UpdateNetwork(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_network", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeleteNetwork(osArgStr(args, 0)))
	})
	d("subnets", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).Subnets())
	})
	d("get_subnet", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetSubnet(osArgStr(args, 0)))
	})
	d("create_subnet", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreateSubnet(osArgAttrs(args, 0)))
	})
	d("update_subnet", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).UpdateSubnet(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_subnet", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeleteSubnet(osArgStr(args, 0)))
	})
	d("ports", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).Ports())
	})
	d("get_port", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetPort(osArgStr(args, 0)))
	})
	d("create_port", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreatePort(osArgAttrs(args, 0)))
	})
	d("update_port", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).UpdatePort(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_port", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeletePort(osArgStr(args, 0)))
	})
	d("routers", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).Routers())
	})
	d("get_router", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetRouter(osArgStr(args, 0)))
	})
	d("create_router", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreateRouter(osArgAttrs(args, 0)))
	})
	d("update_router", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).UpdateRouter(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_router", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeleteRouter(osArgStr(args, 0)))
	})
	d("security_groups", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).SecurityGroups())
	})
	d("get_security_group", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetSecurityGroup(osArgStr(args, 0)))
	})
	d("create_security_group", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreateSecurityGroup(osArgAttrs(args, 0)))
	})
	d("update_security_group", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).UpdateSecurityGroup(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_security_group", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeleteSecurityGroup(osArgStr(args, 0)))
	})
	d("security_group_rules", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).SecurityGroupRules())
	})
	d("get_security_group_rule", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetSecurityGroupRule(osArgStr(args, 0)))
	})
	d("create_security_group_rule", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreateSecurityGroupRule(osArgAttrs(args, 0)))
	})
	d("delete_security_group_rule", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeleteSecurityGroupRule(osArgStr(args, 0)))
	})
	d("floating_ips", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(n(self).FloatingIPs())
	})
	d("get_floating_ip", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).GetFloatingIP(osArgStr(args, 0)))
	})
	d("create_floating_ip", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).CreateFloatingIP(osArgAttrs(args, 0)))
	})
	d("update_floating_ip", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(n(self).UpdateFloatingIP(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_floating_ip", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(n(self).DeleteFloatingIP(osArgStr(args, 0)))
	})
}

// registerOpenStackBlockStorage installs the Cinder (block storage) surface:
// volumes, snapshots and volume types.
func (vm *VM) registerOpenStackBlockStorage(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	b := func(self object.Value) *openstack.BlockStorage { return self.(*OpenStackBlockStorage).s }

	d("volumes", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(b(self).Volumes())
	})
	d("get_volume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).GetVolume(osArgStr(args, 0)))
	})
	d("create_volume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).CreateVolume(osArgAttrs(args, 0)))
	})
	d("update_volume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).UpdateVolume(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_volume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(b(self).DeleteVolume(osArgStr(args, 0)))
	})
	d("snapshots", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(b(self).Snapshots())
	})
	d("get_snapshot", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).GetSnapshot(osArgStr(args, 0)))
	})
	d("create_snapshot", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).CreateSnapshot(osArgAttrs(args, 0)))
	})
	d("update_snapshot", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).UpdateSnapshot(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_snapshot", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(b(self).DeleteSnapshot(osArgStr(args, 0)))
	})
	d("volume_types", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(b(self).VolumeTypes())
	})
	d("get_volume_type", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).GetVolumeType(osArgStr(args, 0)))
	})
	d("create_volume_type", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).CreateVolumeType(osArgAttrs(args, 0)))
	})
	d("update_volume_type", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(b(self).UpdateVolumeType(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_volume_type", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(b(self).DeleteVolumeType(osArgStr(args, 0)))
	})
}

// registerOpenStackObjectStorage installs the Swift (object storage) surface:
// containers and objects. Object bodies cross the boundary as Ruby Strings.
func (vm *VM) registerOpenStackObjectStorage(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	o := func(self object.Value) *openstack.ObjectStorage { return self.(*OpenStackObjectStorage).s }

	d("containers", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(o(self).Containers())
	})
	d("create_container", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(o(self).CreateContainer(osArgStr(args, 0)))
	})
	d("delete_container", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(o(self).DeleteContainer(osArgStr(args, 0)))
	})
	d("objects", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResources(o(self).Objects(osArgStr(args, 0)))
	})
	d("get_object", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osBytes(o(self).GetObject(osArgStr(args, 0), osArgStr(args, 1)))
	})
	d("put_object", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(o(self).PutObject(osArgStr(args, 0), osArgStr(args, 1), osArgReader(args, 2)))
	})
	d("delete_object", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(o(self).DeleteObject(osArgStr(args, 0), osArgStr(args, 1)))
	})
}

// registerOpenStackImage installs the Glance (image) surface: image records and
// their binary data. UpdateImage is intentionally not wrapped — its library
// signature takes a typed images.UpdateOpts (a JSON-patch operation list), not a
// Resource, so it has no idiomatic attribute-hash form.
func (vm *VM) registerOpenStackImage(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	i := func(self object.Value) *openstack.Image { return self.(*OpenStackImage).s }

	d("images", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(i(self).Images())
	})
	d("get_image", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(i(self).GetImage(osArgStr(args, 0)))
	})
	d("create_image", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(i(self).CreateImage(osArgAttrs(args, 0)))
	})
	d("delete_image", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(i(self).DeleteImage(osArgStr(args, 0)))
	})
	d("upload_image", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(i(self).UploadImage(osArgStr(args, 0), osArgReader(args, 1)))
	})
}

// registerOpenStackIdentity installs the Keystone (identity) surface: projects,
// users, roles and domains.
func (vm *VM) registerOpenStackIdentity(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	id := func(self object.Value) *openstack.Identity { return self.(*OpenStackIdentity).s }

	d("projects", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(id(self).Projects())
	})
	d("get_project", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).GetProject(osArgStr(args, 0)))
	})
	d("create_project", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).CreateProject(osArgAttrs(args, 0)))
	})
	d("update_project", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).UpdateProject(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_project", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(id(self).DeleteProject(osArgStr(args, 0)))
	})
	d("users", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(id(self).Users())
	})
	d("get_user", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).GetUser(osArgStr(args, 0)))
	})
	d("create_user", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).CreateUser(osArgAttrs(args, 0)))
	})
	d("update_user", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).UpdateUser(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_user", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(id(self).DeleteUser(osArgStr(args, 0)))
	})
	d("roles", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(id(self).Roles())
	})
	d("get_role", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).GetRole(osArgStr(args, 0)))
	})
	d("create_role", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).CreateRole(osArgAttrs(args, 0)))
	})
	d("update_role", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).UpdateRole(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_role", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(id(self).DeleteRole(osArgStr(args, 0)))
	})
	d("domains", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.osResources(id(self).Domains())
	})
	d("get_domain", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).GetDomain(osArgStr(args, 0)))
	})
	d("create_domain", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).CreateDomain(osArgAttrs(args, 0)))
	})
	d("update_domain", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osResource(id(self).UpdateDomain(osArgStr(args, 0), osArgAttrs(args, 1)))
	})
	d("delete_domain", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.osVoid(id(self).DeleteDomain(osArgStr(args, 0)))
	})
}
