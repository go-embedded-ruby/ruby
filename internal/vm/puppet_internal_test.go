// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	puppet "github.com/go-ruby-puppet/puppet"
)

// puppetRun runs a Ruby program with `require "puppet"` prepended.
func puppetRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"puppet\"\n"+body)
}

// orderedManifest declares a package, a service (with ensure, a require
// metaparameter and a tag) and a notify, plus an explicit ordering edge.
const orderedManifest = "m = <<~'PP'\n" +
	"package { 'nginx': ensure => installed }\n" +
	"service { 'nginx':\n" +
	"  ensure  => running,\n" +
	"  require => Package['nginx'],\n" +
	"  tag     => 'web',\n" +
	"}\n" +
	"notify { 'done': message => 'ok' }\n" +
	"Service['nginx'] -> Notify['done']\n" +
	"PP\n"

// TestPuppetParse covers Puppet.parse: valid manifest, syntax error, missing arg.
func TestPuppetParse(t *testing.T) {
	got := puppetRun(t, orderedManifest+`
puts Puppet.parse(m)
begin; Puppet.parse("package { 'nginx': ensure =>"); rescue => e; puts e.class; end
begin; Puppet.parse; rescue => e; puts e.class; end
`)
	want := "true\nPuppet::ParseError\nArgumentError"
	if got != want {
		t.Fatalf("parse:\n got=%q\nwant=%q", got, want)
	}
}

// TestPuppetCompileCatalog drives compilation and the catalog surface.
func TestPuppetCompileCatalog(t *testing.T) {
	got := puppetRun(t, orderedManifest+`
cat = Puppet.compile(m)
puts cat.class
puts cat.size
puts cat.length
puts cat.resources.map(&:ref).sort.inspect
puts cat.resource("Service[nginx]").ref
puts cat.resource("Service[absent]").inspect
puts cat.edges.include?(["Service[nginx]", "Notify[done]"])
puts cat.to_json.include?("default")
puts cat.logs.class
`)
	want := "Puppet::Resource::Catalog\n3\n3\n" +
		`["Notify[done]", "Package[nginx]", "Service[nginx]"]` + "\n" +
		"Service[nginx]\nnil\ntrue\ntrue\nArray"
	if got != want {
		t.Fatalf("catalog:\n got=%q\nwant=%q", got, want)
	}
}

// TestPuppetResourceMethods covers the Puppet::Resource surface on Service[nginx].
func TestPuppetResourceMethods(t *testing.T) {
	got := puppetRun(t, orderedManifest+`
r = Puppet.compile(m).resource("Service[nginx]")
puts r.type
puts r.title
puts r.ref
puts r.to_s
puts r.class
puts r["ensure"]
puts r["nope"].inspect
puts r.parameters["ensure"]
puts r.tags.inspect
`)
	want := "Service\nnginx\nService[nginx]\nService[nginx]\nPuppet::Resource\nrunning\nnil\nrunning\n[\"web\"]"
	if got != want {
		t.Fatalf("resource methods:\n got=%q\nwant=%q", got, want)
	}
}

// TestPuppetFactsInterpolation covers $facts interpolation via compile facts.
func TestPuppetFactsInterpolation(t *testing.T) {
	got := puppetRun(t, `
m = <<~'PP'
notify { 'f': message => "os is ${facts['os']}" }
PP
cat = Puppet.compile(m, facts: { "os" => "linux" })
puts cat.resource("Notify[f]")["message"]
`)
	if got != "os is linux" {
		t.Fatalf("facts interp: got=%q want=%q", got, "os is linux")
	}
}

// TestPuppetLogs covers the compile-log surface: a notice() call produces a log
// entry exposed through Catalog#logs.
func TestPuppetLogs(t *testing.T) {
	got := puppetRun(t, `
cat = Puppet.compile("notice('hello world')")
puts cat.logs.size
l = cat.logs.first
puts l[:level]
puts l[:message]
`)
	want := "1\nnotice\nhello world"
	if got != want {
		t.Fatalf("logs:\n got=%q\nwant=%q", got, want)
	}
}

// puppetHieraFixture writes a Hiera 5 config binding demo::greeting, returning the
// hiera.yaml path.
func puppetHieraFixture(t *testing.T, value string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "version: 5\ndefaults:\n  datadir: data\n  data_hash: yaml_data\nhierarchy:\n  - name: common\n    path: common.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "hiera.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "common.yaml"), []byte("demo::greeting: "+value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "hiera.yaml")
}

// TestPuppetHiera covers automatic class-parameter data binding via hiera_config.
func TestPuppetHiera(t *testing.T) {
	cfg := puppetHieraFixture(t, `"hi from hiera"`)
	got := runSrc(t, fmt.Sprintf("require \"puppet\"\ncfg = %q\n", cfg)+`
m = <<~'PP'
class demo (String $greeting) {
  notify { 'greet': message => $greeting }
}
include demo
PP
cat = Puppet.compile(m, facts: { "os" => "Debian" }, hiera_config: cfg)
puts cat.resource("Notify[greet]")["message"]
`)
	if got != "hi from hiera" {
		t.Fatalf("hiera binding: got=%q want=%q", got, "hi from hiera")
	}
}

// TestPuppetErrors covers the raising paths: evaluation error (duplicate
// declaration), a bad hiera_config path, and the missing-argument ArgumentErrors.
func TestPuppetErrors(t *testing.T) {
	cases := []struct{ body, want string }{
		{`begin; Puppet.compile("notify { 'x': }\nnotify { 'x': }"); rescue => e; puts e.class; end`, "Puppet::Error"},
		{`begin; Puppet.compile("notify { 'x': }", hiera_config: "/no/such/hiera.yaml"); rescue => e; puts e.class; end`, "Puppet::Error"},
		{`begin; Puppet.compile; rescue => e; puts e.class; end`, "ArgumentError"},
		{`cat = Puppet.compile("notify { 'x': }"); begin; cat.resource; rescue => e; puts e.class; end`, "ArgumentError"},
		{`r = Puppet.compile("notify { 'x': }").resource("Notify[x]"); begin; r.send(:[]); rescue => e; puts e.class; end`, "ArgumentError"},
	}
	for _, c := range cases {
		if got := puppetRun(t, c.body); got != c.want {
			t.Fatalf("%s\n got=%q want=%q", c.body, got, c.want)
		}
	}
}

// TestPuppetStringKwargs covers string-keyed kwargs and the node_name option.
func TestPuppetStringKwargs(t *testing.T) {
	got := puppetRun(t, `puts Puppet.compile("notify { 'x': }", "node_name" => "n1").to_json.include?("n1")`)
	if got != "true" {
		t.Fatalf("string kwargs/node_name: got=%q want=true", got)
	}
}

// TestPuppetStringers covers the object.Value marker methods on both wrappers.
func TestPuppetStringers(t *testing.T) {
	got := puppetRun(t, orderedManifest+`
cat = Puppet.compile(m)
r = cat.resource("Service[nginx]")
puts "yes" if cat
puts "yes" if r
`)
	if got != "yes\nyes" {
		t.Fatalf("truthy: got=%q", got)
	}
	c := &PuppetCatalog{}
	if c.ToS() != "#<Puppet::Resource::Catalog>" || c.Inspect() != c.ToS() || !c.Truthy() {
		t.Errorf("catalog stringers = %q / %q / %v", c.ToS(), c.Inspect(), c.Truthy())
	}
	pr := &PuppetResource{r: &puppet.Resource{Type: "Service", Title: "nginx"}}
	if pr.ToS() != "Service[nginx]" || pr.Inspect() != "#<Puppet::Resource Service[nginx]>" || !pr.Truthy() {
		t.Errorf("resource stringers = %q / %q / %v", pr.ToS(), pr.Inspect(), pr.Truthy())
	}
}
