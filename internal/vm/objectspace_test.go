// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestObjectSpaceReflection covers ObjectSpace.each_object, count_objects,
// _id2ref and the finalizer API. Asserted against MRI Ruby 4.0.5.
func TestObjectSpaceReflection(t *testing.T) {
	cases := []struct{ src, want string }{
		// each_object(klass) finds instances created and still referenced.
		{`k = Class.new; o = k.new; n = ObjectSpace.each_object(k) { |x| }; p [n, o.class == k]`, "[1, true]\n"},
		{`k = Class.new; o = k.new; seen = []; ObjectSpace.each_object(k) { |x| seen << x.equal?(o) }; p seen`, "[true]\n"},
		// each_object(Class) / each_object(Module) find freshly built classes/modules.
		{`k = Class.new; found = false; ObjectSpace.each_object(Class) { |c| found = true if c.equal?(k) }; p found`, "true\n"},
		{`m = Module.new; found = false; ObjectSpace.each_object(Module) { |c| found = true if c.equal?(m) }; p found`, "true\n"},
		// each_object(Class) does not yield modules.
		{`m = Module.new; bad = false; ObjectSpace.each_object(Class) { |c| bad = true if c.equal?(m) }; p bad`, "false\n"},
		// No block -> Enumerator whose #each drives the walk.
		{`k = Class.new; o = k.new; e = ObjectSpace.each_object(k); p [e.class, e.each {}]`, "[Enumerator, 1]\n"},
		{`p ObjectSpace.each_object.class`, "Enumerator\n"},
		// Instances built through #allocate are tracked too.
		{`k = Class.new; o = k.allocate; n = ObjectSpace.each_object(k) { |x| }; p n`, "1\n"},

		// count_objects: shape + invariants; a supplied hash is cleared and reused.
		{`h = ObjectSpace.count_objects; p [h.class, h.key?(:TOTAL), h.key?(:FREE), h[:TOTAL] >= h[:FREE]]`, "[Hash, true, true, true]\n"},
		{`h = {stale: 1}; r = ObjectSpace.count_objects(h); p [r.equal?(h), h.key?(:stale), h.key?(:T_OBJECT)]`, "[true, false, true]\n"},

		// _id2ref round-trips.
		{`p ObjectSpace._id2ref(nil.object_id)`, "nil\n"},
		{`p ObjectSpace._id2ref(true.object_id)`, "true\n"},
		{`p ObjectSpace._id2ref(false.object_id)`, "false\n"},
		{`p ObjectSpace._id2ref(1.object_id)`, "1\n"},
		{`p ObjectSpace._id2ref((-42).object_id)`, "-42\n"},
		{`s = "hi"; p ObjectSpace._id2ref(s.object_id).equal?(s)`, "true\n"},
		{`p ObjectSpace._id2ref(:sym.object_id).equal?(:sym)`, "true\n"},

		// define_finalizer returns [0, callable]; a block is the callable.
		{`h = -> id { id }; p ObjectSpace.define_finalizer(Object.new, h) == [0, h]`, "true\n"},
		{`o = Object.new; m = o.method(:object_id); p ObjectSpace.define_finalizer(o, m) == [0, m]`, "true\n"},
		// dedup: an == callable is not re-added; the first is returned.
		{`o = Object.new; p1 = proc { |i| }; p2 = p1.dup; ObjectSpace.define_finalizer(o, p1); r = ObjectSpace.define_finalizer(o, p2); p r[1].equal?(p1)`, "true\n"},
		// undefine_finalizer returns the object; a never-finalized object is fine.
		{`o = Object.new; p ObjectSpace.undefine_finalizer(o).equal?(o)`, "true\n"},
		{`o = Object.new; ObjectSpace.define_finalizer(o, proc { |i| }); p ObjectSpace.undefine_finalizer(o).equal?(o)`, "true\n"},

		// garbage_collect / start return nil (kwargs and block ignored).
		{`p ObjectSpace.garbage_collect(full_mark: true)`, "nil\n"},
		{`p ObjectSpace.start { }`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errCases := []struct{ src, want string }{
		{`ObjectSpace.define_finalizer`, "ArgumentError"},                           // no object
		{`ObjectSpace.define_finalizer(:blah) { 1 }`, "ArgumentError"},              // immediate object
		{`ObjectSpace.define_finalizer(Object.new)`, "ArgumentError"},               // no callable, no block
		{`ObjectSpace.define_finalizer(Object.new, Object.new)`, "ArgumentError"},   // callable has no #call
		{`ObjectSpace.undefine_finalizer`, "ArgumentError"},                         // no object
		{`o = Object.new.freeze; ObjectSpace.undefine_finalizer(o)`, "FrozenError"}, // frozen object
		{`ObjectSpace.each_object(5)`, "TypeError"},                                 // non-module argument
		{`ObjectSpace._id2ref`, "ArgumentError"},                                    // no id
		{`ObjectSpace._id2ref(1 << 60)`, "RangeError"},                              // unknown id
		{`ObjectSpace.count_objects(5)`, "TypeError"},                               // non-Hash argument
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %s", c.src, err, c.want)
		}
	}
}

// TestObjectSpaceWeakMap covers ObjectSpace::WeakMap (identity semantics) and its
// full accessor/iteration/inspect surface. Asserted against MRI Ruby 4.0.5.
func TestObjectSpaceWeakMap(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p ObjectSpace::WeakMap.include?(Enumerable)`, "true\n"},
		{`m = ObjectSpace::WeakMap.new; k = "K"; v = "V"; p [(m[k] = v), m[k]]`, "[\"V\", \"V\"]\n"},
		{`m = ObjectSpace::WeakMap.new; p m[Object.new]`, "nil\n"}, // miss
		// identity: two distinct equal Strings are different keys.
		{`m = ObjectSpace::WeakMap.new; a, b = %w[a a].map(&:upcase); m[a] = "x"; p m[b]`, "nil\n"},
		// include?/key?/member? (value nil still counts as present).
		{`m = ObjectSpace::WeakMap.new; k = Object.new; m[k] = nil; p [m.include?(k), m.key?(k), m.member?(k), m.size]`, "[true, true, true, 1]\n"},
		{`m = ObjectSpace::WeakMap.new; p m.include?(Object.new)`, "false\n"},
		// size / length; overwrite keeps size.
		{`m = ObjectSpace::WeakMap.new; k = Object.new; m[k] = 1; m[k] = 2; p [m.size, m.length, m[k]]`, "[1, 1, 2]\n"},
		// keys / values.
		{`m = ObjectSpace::WeakMap.new; k1, k2 = Object.new, Object.new; m[k1] = 1; m[k2] = 2; p [m.keys == [k1, k2], m.values]`, "[true, [1, 2]]\n"},
		// delete: hit returns value; miss with block yields key; miss without block nil.
		{`m = ObjectSpace::WeakMap.new; k = Object.new; m[k] = 9; p [m.delete(k), m.key?(k)]`, "[9, false]\n"},
		{`m = ObjectSpace::WeakMap.new; k = Object.new; p m.delete(k) { |yk| yk.equal?(k) ? 5 : 0 }`, "5\n"},
		{`m = ObjectSpace::WeakMap.new; p m.delete(Object.new)`, "nil\n"},
		// each / each_pair / each_key / each_value.
		{`m = ObjectSpace::WeakMap.new; m["A"] = "x"; a = []; m.each { |k, v| a << "#{k}#{v}" }; p a`, "[\"Ax\"]\n"},
		{`m = ObjectSpace::WeakMap.new; p ObjectSpace::WeakMap.instance_method(:each_pair) == ObjectSpace::WeakMap.instance_method(:each)`, "true\n"},
		{`m = ObjectSpace::WeakMap.new; m["A"] = "x"; a = []; m.each_key { |k| a << k }; p a`, "[\"A\"]\n"},
		{`m = ObjectSpace::WeakMap.new; m["A"] = "x"; a = []; m.each_value { |v| a << v }; p a`, "[\"x\"]\n"},
		// each with no block: empty returns self, non-empty raises LocalJumpError.
		{`m = ObjectSpace::WeakMap.new; p m.each.equal?(m)`, "true\n"},
		// aliases share the underlying method.
		{`p ObjectSpace::WeakMap.instance_method(:key?) == ObjectSpace::WeakMap.instance_method(:include?)`, "true\n"},
		{`p ObjectSpace::WeakMap.instance_method(:length) == ObjectSpace::WeakMap.instance_method(:size)`, "true\n"},
		// inspect: empty and populated forms.
		{`m = ObjectSpace::WeakMap.new; p (m.inspect =~ /\A#<ObjectSpace::WeakMap:0x\h+>\z/) == 0`, "true\n"},
		{`m = ObjectSpace::WeakMap.new; m[Object.new] = Object.new; p (m.inspect =~ /\A#<ObjectSpace::WeakMap:0x\h+: #<Object:0x\h+> => #<Object:0x\h+>>\z/) == 0`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// each with no block on a non-empty map raises LocalJumpError.
	if err := runErr(t, `m = ObjectSpace::WeakMap.new; m[Object.new] = 1; m.each`); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
		t.Errorf("WeakMap#each non-empty no block: got %v", err)
	}
}

// TestObjectSpaceWeakKeyMap covers ObjectSpace::WeakKeyMap (equality semantics,
// garbage-collectable-key rule). Asserted against MRI Ruby 4.0.5.
func TestObjectSpaceWeakKeyMap(t *testing.T) {
	cases := []struct{ src, want string }{
		{`m = ObjectSpace::WeakKeyMap.new; k = "K"; v = "V"; p [(m[k] = v), m[k]]`, "[\"V\", \"V\"]\n"},
		{`m = ObjectSpace::WeakKeyMap.new; p m[Object.new]`, "nil\n"}, // miss
		// equality: two distinct equal Strings match.
		{`m = ObjectSpace::WeakKeyMap.new; a, b = %w[a a].map(&:upcase); m[a] = "x"; p m[b]`, "\"x\"\n"},
		// eql? semantics: [1] and [1.0] differ.
		{`m = ObjectSpace::WeakKeyMap.new; m[[1.0]] = "x"; p [m[[1]], m[[1.0]]]`, "[nil, \"x\"]\n"},
		// overwrite existing key keeps one entry.
		{`m = ObjectSpace::WeakKeyMap.new; k = "K"; m[k] = 1; m[k] = 2; p m[k]`, "2\n"},
		// getkey returns the stored (identical) key; miss -> nil.
		{`m = ObjectSpace::WeakKeyMap.new; a, b = %w[a a].map(&:upcase); m[a] = true; p [m.getkey(b).equal?(a), m.getkey("X")]`, "[true, nil]\n"},
		// key? equality; nil value still present.
		{`m = ObjectSpace::WeakKeyMap.new; k = Object.new; m[k] = nil; p m.key?(k)`, "true\n"},
		{`m = ObjectSpace::WeakKeyMap.new; p m.key?(Object.new)`, "false\n"},
		// []= does not dup/freeze String keys.
		{`m = ObjectSpace::WeakKeyMap.new; k = +"a"; m[k] = 1; p [m.getkey("a").equal?(k), m.getkey("a").frozen?]`, "[true, false]\n"},
		// delete: hit returns value; miss with block; miss without block nil.
		{`m = ObjectSpace::WeakKeyMap.new; k = Object.new; m[k] = 9; p [m.delete(k), m.key?(k)]`, "[9, false]\n"},
		{`m = ObjectSpace::WeakKeyMap.new; k = Object.new; p m.delete(k) { |yk| yk.equal?(k) ? 5 : 0 }`, "5\n"},
		{`m = ObjectSpace::WeakKeyMap.new; p m.delete(Object.new)`, "nil\n"},
		// immediates are not garbage-collectable keys: read paths return nil/false, no raise.
		{`m = ObjectSpace::WeakKeyMap.new; p [m[1], m.getkey(:a), m.key?(true), m.delete(nil)]`, "[nil, nil, false, nil]\n"},
		// clear returns self and empties.
		{`m = ObjectSpace::WeakKeyMap.new; k = Object.new; m[k] = 1; r = m.clear; p [r.equal?(m), m.key?(k)]`, "[true, false]\n"},
		// inspect shows only size.
		{`m = ObjectSpace::WeakKeyMap.new; p (m.inspect =~ /\A#<ObjectSpace::WeakKeyMap:0x\h+ size=0>\z/) == 0`, "true\n"},
		{`m = ObjectSpace::WeakKeyMap.new; m["a"] = 1; p (m.inspect =~ /\A#<ObjectSpace::WeakKeyMap:0x\h+ size=1>\z/) == 0`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errCases := []struct{ src, want string }{
		{`ObjectSpace::WeakKeyMap.new[1] = "x"`, "ArgumentError"},                                                   // immediate key
		{`ObjectSpace::WeakKeyMap.new[BasicObject.new] = 1`, "NoMethodError"},                                       // key without #hash
		{`class BadHash; def hash; "not int"; end; end; ObjectSpace::WeakKeyMap.new[BadHash.new] = 1`, "TypeError"}, // #hash not Integer
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %s", c.src, err, c.want)
		}
	}
}
