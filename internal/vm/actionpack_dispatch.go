// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-ruby-actionpack/actionpack/dispatch"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires ActionDispatch::Request (over a Rack env: merged params, path
// parameters, format negotiation) and ActionDispatch::Response (the mutable Rack
// response). The env parsing, param merging and format negotiation are the
// library's; this shell maps values in and out.

// registerACRequest installs the ActionDispatch::Request surface.
func (vm *VM) registerACRequest(cls *RClass) {
	self := func(v object.Value) *dispatch.Request { return v.(*ACRequest).r }

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &ACRequest{r: dispatch.NewRequest(rackEnv(rackArg(args))), cls: cls}
	}}

	// params — the merged strong-parameters view as ActionController::Parameters.
	cls.define("params", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p, err := self(v).Parameters()
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return &ACParams{p: p, cls: vm.cACParameters}
	})

	cls.define("query_parameters", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m, err := self(v).QueryParameters()
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackFromGo(m)
	})
	cls.define("request_parameters", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m, err := self(v).RequestParameters()
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackFromGo(m)
	})
	cls.define("path_parameters", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackFromGo(self(v).PathParameters())
	})
	cls.define("set_path_parameters", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetPathParameters(acAnyMap(lastHashOrNil(args)))
		return object.NilV
	})
	cls.define("controller_name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ControllerName())
	})
	cls.define("action_name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ActionName())
	})
	cls.define("format", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Format())
	})
	cls.define("request_method", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RequestMethod())
	})
	cls.define("path", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).PathInfo())
	})
}

// registerACResponse installs the ActionDispatch::Response surface.
func (vm *VM) registerACResponse(cls *RClass) {
	self := func(v object.Value) *dispatch.Response { return v.(*ACResponse).r }

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ACResponse{r: dispatch.NewResponse(), cls: cls}
	}}

	cls.define("status", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Status()))
	})
	cls.define("status=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetStatus(apInt(apArg(args, 0)))
		return apArg(args, 0)
	})
	cls.define("write", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			self(v).Write(rackStr(a))
		}
		return object.NilV
	})
	cls.define("body", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).BodyString())
	})
	cls.define("headers", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackHeadersToHash(self(v).Headers())
	})
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return rackFromGo(self(v).Headers().Get(apStr(apArg(args, 0))))
	})
	cls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).Headers().Set(apStr(args[0]), rackStr(args[1]))
		return args[1]
	})
	cls.define("content_type=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetContentType(rackStr(apArg(args, 0)))
		return apArg(args, 0)
	})
	cls.define("redirect", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		status := 302
		if len(args) > 1 {
			status = apInt(args[1])
		}
		self(v).Redirect(rackStr(args[0]), status)
		return object.NilV
	})
}
