// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestActiveStorageConstants covers the ActiveStorage module, its Blob / Service /
// Attachment / Attached::One / Attached::Many classes and the error tree
// (require "active_storage").
func TestActiveStorageConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "active_storage"; p ActiveStorage.is_a?(Module)`, "true\n"},
		{`p require "active_storage"`, "true\n"},
		{`require "active_storage"; p require "active_storage"`, "false\n"},
		{`p require "activestorage"`, "true\n"},
		{`require "active_storage"; p ActiveStorage::IntegrityError < ActiveStorage::Error`, "true\n"},
		{`require "active_storage"; p ActiveStorage::InvalidSignature < ActiveStorage::Error`, "true\n"},
		{`require "active_storage"; p ActiveStorage::UnattachableError < ActiveStorage::Error`, "true\n"},
		{`require "active_storage"; p ActiveStorage::FileNotFoundError < ActiveStorage::Error`, "true\n"},
		{`require "active_storage"; p ActiveStorage::Error < StandardError`, "true\n"},
		{`require "active_storage"; p ActiveStorage::Service::DiskService < ActiveStorage::Service`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageBlob covers Blob.create_and_upload! (byte size, base36 sharded
// key, base64 MD5 checksum, inferred content type), #download, #download_chunk,
// #find, and the value's class / inspect / truthiness.
func TestActiveStorageBlob(t *testing.T) {
	cases := []struct{ src, want string }{
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "greeting.txt")
p b.class`, "ActiveStorage::Blob\n"},
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "greeting.txt")
p b.filename; p b.content_type; p b.byte_size; p b.checksum; p b.service_name`,
			"\"greeting.txt\"\n\"text/plain\"\n5\n\"XUFAKrxLKna5cZ2REBfFkg==\"\n\"local\"\n"},
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "greeting.txt")
p b.key.length`, "28\n"},
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "greeting.txt")
p b.download; p b.download_chunk(0, 3)`, "\"hello\"\n\"hel\"\n"},
		// download_chunk with a missing length reads to end-of-length 0; a
		// non-integer offset/length degrades to 0.
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "greeting.txt")
p b.download_chunk(0); p b.download_chunk("x", "y")`, "\"\"\n\"\"\n"},
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hi", filename: "a.txt")
p ActiveStorage::Blob.find(b.id).download`, "\"hi\"\n"},
		// Inspect + truthiness of the blob value.
		{`b = ActiveStorage::Blob.create_and_upload!(io: "x", filename: "a.txt")
p b.to_s.start_with?("#<ActiveStorage::Blob key: "); puts(b ? "yes" : "no")`, "true\nyes\n"},
		// An explicit key: and content_type: pass through unchanged; a String-keyed
		// options Hash is accepted as well as a symbol-keyed one.
		{`b = ActiveStorage::Blob.create_and_upload!("io" => "z", "filename" => "n.dat", "key" => "0000000000000000000000000000", "content_type" => "application/x-thing")
p b.key; p b.content_type`, "\"0000000000000000000000000000\"\n\"application/x-thing\"\n"},
		// build_after_upload uploads but does not persist a row (id stays 0).
		{`b = ActiveStorage::Blob.build_after_upload(io: "hey", filename: "a.txt")
p b.id; p b.download`, "0\n\"hey\"\n"},
		// #purge removes the stored object and the row.
		{`b = ActiveStorage::Blob.create_and_upload!(io: "bye", filename: "a.txt")
svc = ActiveStorage::Blob.service
key = b.key
p b.purge; p svc.exist?(key)`, "nil\nfalse\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageSignedID covers #signed_id and Blob.find_signed round-tripping
// through the deterministic HMAC signer.
func TestActiveStorageSignedID(t *testing.T) {
	cases := []struct{ src, want string }{
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "a.txt")
sid = b.signed_id
p sid.start_with?("blob_id.")
p ActiveStorage::Blob.find_signed(sid).id == b.id`, "true\ntrue\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageService covers the ActiveStorage::Service backend surface
// (name / exist? / size / url / delete / upload) over the built-in disk service.
func TestActiveStorageService(t *testing.T) {
	cases := []struct{ src, want string }{
		{`svc = ActiveStorage::Blob.service; p svc.class; p svc.name`,
			"ActiveStorage::Service\n\"local\"\n"},
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "a.txt")
svc = ActiveStorage::Blob.service
p svc.exist?(b.key); p svc.size(b.key)`, "true\n5\n"},
		{`svc = ActiveStorage::Blob.service
p svc.exist?("0000000000000000000000000000")`, "false\n"},
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "a.txt")
svc = ActiveStorage::Blob.service
svc.delete(b.key); p svc.exist?(b.key)`, "false\n"},
		// Deleting a missing key is idempotent (no error).
		{`svc = ActiveStorage::Blob.service
p svc.delete("0000000000000000000000000000")`, "nil\n"},
		// A direct upload (no checksum) then read-back through exist?/size.
		{`svc = ActiveStorage::Blob.service
svc.upload("abcd1234efgh5678ijkl9012mnop", "data")
p svc.exist?("abcd1234efgh5678ijkl9012mnop"); p svc.size("abcd1234efgh5678ijkl9012mnop")`, "true\n4\n"},
		// A service URL is disposition-annotated and routes under the disk engine.
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "a.txt")
svc = ActiveStorage::Blob.service
p svc.url(b.key, disposition: "attachment").start_with?("/rails/active_storage/disk/")`, "true\n"},
		// A blob's own #url and #service.
		{`b = ActiveStorage::Blob.create_and_upload!(io: "hello", filename: "a.txt")
p b.url.start_with?("/rails/active_storage/disk/"); p b.service.name`, "true\n\"local\"\n"},
		// Inspect + truthiness of the service value.
		{`svc = ActiveStorage::Blob.service
p svc.to_s.start_with?("#<ActiveStorage::Service name: "); puts(svc ? "y" : "n")`, "true\ny\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageDirectUpload covers Blob.create_before_direct_upload! (a
// persisted blob with a known byte_size/checksum and no uploaded bytes), including
// the byte_size-omitted default.
func TestActiveStorageDirectUpload(t *testing.T) {
	cases := []struct{ src, want string }{
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.bin", byte_size: 3, checksum: "abc")
p b.byte_size; p b.checksum; p b.id > 0`, "3\n\"abc\"\ntrue\n"},
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.bin", checksum: "abc")
p b.byte_size`, "0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageOneAttached covers the has_one_attached proxy
// (ActiveStorage::Attached::One): attach / attached? / blob / attachment / detach
// / purge and the Attachment readers, exercising the *Blob, Upload-Hash and
// signed-id attachable forms.
func TestActiveStorageOneAttached(t *testing.T) {
	prelude := `one = ActiveStorage::Attached::One.new(record_type: "User", record_id: 1, name: "avatar")
b = ActiveStorage::Blob.create_and_upload!(io: "img", filename: "a.png")
`
	cases := []struct{ src, want string }{
		{prelude + `p one.attached?`, "false\n"},
		{prelude + `p one.blob; p one.attachment`, "nil\nnil\n"},
		{prelude + `att = one.attach(b)
p att.class; p att.name; p att.record_type; p att.record_id; p att.blob_id == b.id`,
			"ActiveStorage::Attachment\n\"avatar\"\n\"User\"\n1\ntrue\n"},
		{prelude + `one.attach(b); p one.attached?; p one.blob.filename; p one.attachment.name`,
			"true\n\"a.png\"\n\"avatar\"\n"},
		// Attaching a second blob replaces the first (has_one).
		{prelude + `one.attach(b)
b2 = ActiveStorage::Blob.create_and_upload!(io: "img2", filename: "b.png")
one.attach(b2); p one.blob.filename`, "\"b.png\"\n"},
		// Attach an Upload Hash { io:, filename: }.
		{prelude + `att = one.attach(io: "raw", filename: "c.png"); p att.name; p one.blob.download`,
			"\"avatar\"\n\"raw\"\n"},
		// Attach by signed id (a String attachable).
		{prelude + `one.attach(b.signed_id); p one.blob.filename`, "\"a.png\"\n"},
		{prelude + `one.attach(b); one.detach; p one.attached?`, "false\n"},
		{prelude + `one.attach(b); one.purge; p one.attached?`, "false\n"},
		// detach / purge with nothing attached are no-ops.
		{prelude + `one.detach; one.purge; p one.attached?`, "false\n"},
		// Inspect + truthiness of the proxy.
		{prelude + `p one.to_s; puts(one ? "y" : "n")`, "\"#<ActiveStorage::Attached::One>\"\ny\n"},
		// record_id may be omitted (defaults to 0).
		{`one = ActiveStorage::Attached::One.new(record_type: "User", name: "avatar"); p one.attached?`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageManyAttached covers the has_many_attached proxy
// (ActiveStorage::Attached::Many): attach / attachments / attached? / blobs /
// detach / purge.
func TestActiveStorageManyAttached(t *testing.T) {
	prelude := `many = ActiveStorage::Attached::Many.new(record_type: "Post", record_id: 5, name: "images")
b1 = ActiveStorage::Blob.create_and_upload!(io: "one", filename: "1.png")
b2 = ActiveStorage::Blob.create_and_upload!(io: "two", filename: "2.png")
`
	cases := []struct{ src, want string }{
		{prelude + `p many.attached?; p many.attachments.length; p many.blobs.length`, "false\n0\n0\n"},
		{prelude + `atts = many.attach(b1, b2)
p atts.length; p atts.map { |a| a.name }`, "2\n[\"images\", \"images\"]\n"},
		{prelude + `many.attach(b1, b2)
p many.attached?; p many.attachments.length; p many.blobs.map { |b| b.filename }`,
			"true\n2\n[\"1.png\", \"2.png\"]\n"},
		{prelude + `many.attach(b1, b2); many.detach; p many.attached?`, "false\n"},
		{prelude + `many.attach(b1, b2); many.purge; p many.attached?`, "false\n"},
		{prelude + `many.detach; p many.attached?`, "false\n"},
		{prelude + `p many.to_s; puts(many ? "y" : "n")`, "\"#<ActiveStorage::Attached::Many>\"\ny\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveStorageAttachmentPurge covers Attachment#purge (removing the join
// record and purging its blob).
func TestActiveStorageAttachmentPurge(t *testing.T) {
	src := `one = ActiveStorage::Attached::One.new(record_type: "User", record_id: 9, name: "doc")
b = ActiveStorage::Blob.create_and_upload!(io: "d", filename: "d.txt")
att = one.attach(b)
att.purge
p one.attached?`
	if got := eval(t, src); got != "false\n" {
		t.Errorf("got=%q want=%q", got, "false\n")
	}
}

// TestActiveStorageInspect covers each wrapper value's #inspect (p) and the
// Attachment readers not exercised elsewhere (#id / #to_s / truthiness).
func TestActiveStorageInspect(t *testing.T) {
	// A blob's inspect embeds its (deterministic) key, so assert only the prefix.
	if got := eval(t, `b = ActiveStorage::Blob.create_and_upload!(io: "x", filename: "a.txt"); p b.inspect.start_with?("#<ActiveStorage::Blob key: ")`); got != "true\n" {
		t.Errorf("blob inspect got=%q", got)
	}
	if got := eval(t, `p ActiveStorage::Blob.service.inspect`); got != "\"#<ActiveStorage::Service name: local>\"\n" {
		t.Errorf("service inspect got=%q", got)
	}
	if got := eval(t, `p ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a").inspect`); got != "\"#<ActiveStorage::Attached::One>\"\n" {
		t.Errorf("one inspect got=%q", got)
	}
	if got := eval(t, `p ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a").inspect`); got != "\"#<ActiveStorage::Attached::Many>\"\n" {
		t.Errorf("many inspect got=%q", got)
	}
	// The attachment value: #id, #inspect, #to_s and truthiness.
	src := `one = ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a")
b = ActiveStorage::Blob.create_and_upload!(io: "x", filename: "a.txt")
att = one.attach(b)
p att.id > 0
p att
p att.to_s
puts(att ? "y" : "n")`
	want := "true\n#<ActiveStorage::Attachment name: a>\n\"#<ActiveStorage::Attachment name: a>\"\ny\n"
	if got := eval(t, src); got != want {
		t.Errorf("attachment inspect got=%q want=%q", got, want)
	}
}

// TestActiveStorageErrors covers the mapped ActiveStorage exception tree: an
// unattachable value, a missing blob, a tampered signed id, an integrity
// mismatch, an unregistered service, a non-String io:, and a missing stored
// object (the generic error arm).
func TestActiveStorageErrors(t *testing.T) {
	cases := []struct{ src, class string }{
		// A non-String io: is a TypeError.
		{`ActiveStorage::Blob.create_and_upload!(io: 5, filename: "a.txt")`, "TypeError"},
		// A missing blob id.
		{`ActiveStorage::Blob.find(999)`, "ActiveStorage::FileNotFoundError"},
		// A tampered / malformed signed id.
		{`ActiveStorage::Blob.find_signed("garbage")`, "ActiveStorage::InvalidSignature"},
		// find_signed with no argument (empty signed id) also fails verification.
		{`ActiveStorage::Blob.find_signed`, "ActiveStorage::InvalidSignature"},
		// An unattachable value.
		{`one = ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a"); one.attach(42)`,
			"ActiveStorage::UnattachableError"},
		// attach with no argument (nil attachable) is unattachable too.
		{`one = ActiveStorage::Attached::One.new(record_type: "U", record_id: 1, name: "a"); one.attach`,
			"ActiveStorage::UnattachableError"},
		{`many = ActiveStorage::Attached::Many.new(record_type: "U", record_id: 1, name: "a"); many.attach(42)`,
			"ActiveStorage::UnattachableError"},
		// An integrity mismatch on a checksummed upload.
		{`ActiveStorage::Blob.service.upload("abcd1234efgh5678ijkl9012mnop", "data", checksum: "wrong")`,
			"ActiveStorage::IntegrityError"},
		// An unregistered service (create-and-upload can build the row but not
		// resolve the service to store the bytes).
		{`ActiveStorage::Blob.create_and_upload!(io: "x", filename: "a.txt", service_name: "nope")`,
			"ActiveStorage::Error"},
		{`ActiveStorage::Blob.build_after_upload(io: "x", filename: "a.txt", service_name: "nope")`,
			"ActiveStorage::Error"},
		// A blob whose service is unknown: #url / #download / #download_chunk /
		// #service / #purge all fail to resolve it.
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c", service_name: "nope"); b.url`,
			"ActiveStorage::Error"},
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c", service_name: "nope"); b.download`,
			"ActiveStorage::Error"},
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c", service_name: "nope"); b.download_chunk(0, 1)`,
			"ActiveStorage::Error"},
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c", service_name: "nope"); b.service`,
			"ActiveStorage::Error"},
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c", service_name: "nope"); b.purge`,
			"ActiveStorage::Error"},
		// A persisted-but-not-uploaded blob on a real service: the object is
		// missing, a generic (non-sentinel) storage error.
		{`b = ActiveStorage::Blob.create_before_direct_upload!(filename: "a.txt", byte_size: 1, checksum: "c"); b.download`,
			"ActiveStorage::Error"},
	}
	for _, c := range cases {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q got class=%q want=%q", c.src, class, c.class)
		}
	}
}
