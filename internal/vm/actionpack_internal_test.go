// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-ruby-actionpack/actionpack/controller"
	"github.com/go-ruby-actionpack/actionpack/dispatch"
	"github.com/go-ruby-actionpack/actionpack/parameters"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestActionPackValueInspect covers the value wrappers' string/truthy surface.
func TestActionPackValueInspect(t *testing.T) {
	rs := &ACRouteSet{}
	mp := &ACMapper{}
	rq := &ACRequest{}
	rp := &ACResponse{}
	for _, c := range []struct {
		v    interface{ ToS() string }
		want string
	}{
		{rs, "#<ActionDispatch::Routing::RouteSet>"},
		{mp, "#<ActionDispatch::Routing::Mapper>"},
		{rq, "#<ActionDispatch::Request>"},
		{rp, "#<ActionDispatch::Response>"},
	} {
		if c.v.ToS() != c.want {
			t.Errorf("ToS = %q, want %q", c.v.ToS(), c.want)
		}
	}
	if !rs.Truthy() || !mp.Truthy() || !rq.Truthy() || !rp.Truthy() {
		t.Error("Truthy should be true")
	}
	if rs.Inspect() != rs.ToS() || mp.Inspect() != mp.ToS() || rq.Inspect() != rq.ToS() || rp.Inspect() != rp.ToS() {
		t.Error("Inspect should equal ToS")
	}
	// ACParams delegates ToS/Inspect to the library's String().
	pw := &ACParams{p: parameters.New(map[string]any{"a": int64(1)})}
	if pw.ToS() != pw.Inspect() || pw.ToS() == "" || !pw.Truthy() {
		t.Errorf("ACParams ToS/Inspect/Truthy: %q", pw.ToS())
	}
}

// TestActionPackHelpers covers the standalone coercion helpers' every branch.
func TestActionPackHelpers(t *testing.T) {
	if apStr(object.NewString("s")) != "s" || apStr(object.Symbol("y")) != "y" || apStr(object.IntValue(3)) != "3" {
		t.Error("apStr")
	}
	if apInt(object.IntValue(5)) != 5 || apInt(object.Float(2.9)) != 2 || apInt(object.NewString("x")) != 0 {
		t.Error("apInt")
	}
	if apArg([]object.Value{object.IntValue(1)}, 0) != object.IntValue(1) || apArg(nil, 2) != object.NilV {
		t.Error("apArg")
	}
	if l := apStrList(object.NewArray(object.Symbol("a"), object.NewString("b"))); len(l) != 2 || l[0] != "a" || l[1] != "b" {
		t.Error("apStrList array")
	}
	if l := apStrList(object.Symbol("only")); len(l) != 1 || l[0] != "only" {
		t.Error("apStrList single")
	}
	if apStrMap(object.NilV) != nil {
		t.Error("apStrMap non-hash")
	}
	h := object.NewHash()
	h.Set(object.Symbol("id"), object.NewString(`\d+`))
	if m := apStrMap(h); m["id"] != `\d+` {
		t.Error("apStrMap hash")
	}
	if lastHashOrNil([]object.Value{object.IntValue(1)}) != nil {
		t.Error("lastHashOrNil non-hash")
	}
	// acErrValue reuses acRubyErrorOf; the ActionNotFound branch is covered here.
	vm := New(nil)
	nf := &controller.ActionNotFound{Controller: "c", Action: "a"}
	if got := vm.acRubyErrorOf(nf); got.Class != "AbstractController::ActionNotFound" {
		t.Errorf("acRubyErrorOf(ActionNotFound) class = %q", got.Class)
	}
	if obj := vm.acErrValue(nf); vm.classOf(obj).name != "AbstractController::ActionNotFound" {
		t.Errorf("acErrValue(ActionNotFound) class = %q", vm.classOf(obj).name)
	}
	if (&acRubyErr{e: RubyError{Class: "X", Message: "y"}}).Error() != "X: y" {
		t.Error("acRubyErr.Error")
	}
}

// --- routing ---------------------------------------------------------------

func TestActionPackRoutingDraw(t *testing.T) {
	src := `
routes = ActionDispatch::Routing::RouteSet.new
routes.draw do
  get "posts", to: "posts#index", as: "posts"
  get "posts/:id", to: "posts#show", as: "post"
  post "posts", to: "posts#create"
  put "posts/:id", to: "posts#update"
  patch "posts/:id", to: "posts#patch_it"
  delete "posts/:id", to: "posts#destroy"
  match "ping", to: "meta#ping", via: [:get, :head]
  root to: "home#index"
end
puts routes.class
r = routes.recognize("GET", "/posts/7")
puts r["controller"]
puts r["action"]
puts r["id"]
puts routes.recognize("GET", "/nope").inspect
puts routes.path("post", id: 3)
puts routes.path_args("post", 9)
puts routes.url_for(controller: "posts", action: "index")
puts routes.routes.length
puts routes.routes[0]["verb"]
`
	got := acRun(t, src)
	want := "ActionDispatch::Routing::RouteSet\nposts\nshow\n7\nnil\n/posts/3\n/posts/9\n/posts\n9\nGET"
	if got != want {
		t.Errorf("routing draw:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackRoutingResources(t *testing.T) {
	src := `
routes = ActionDispatch::Routing::RouteSet.new
routes.draw do
  resources :posts, only: [:index, :show] do
    member { get "preview", to: "posts#preview" }
    collection { get "search", to: "posts#search" }
  end
  resources :photos, except: [:destroy]
  resource :profile
  resources :legacy, path: "old", controller: "legacy_ctrl", param: "key"
  namespace :admin do
    resources :users
  end
  scope path: "api", module: "api", as: "api" do
    get "status", to: "status#show", as: "status"
  end
  constraints id: /\d+/ do
    get "num/:id", to: "num#show"
  end
end
puts routes.recognize("GET", "/posts")["action"]
puts routes.recognize("GET", "/posts/1")["action"]
puts routes.recognize("GET", "/posts/1/preview")["action"]
puts routes.recognize("GET", "/posts/search")["action"]
puts routes.recognize("GET", "/profile")["action"]
puts routes.recognize("GET", "/admin/users")["controller"]
puts routes.recognize("GET", "/api/status")["controller"]
puts routes.recognize("GET", "/num/12")["action"]
`
	got := acRun(t, src)
	want := "index\nshow\npreview\nsearch\nshow\nadmin/users\napi/status\nshow"
	if got != want {
		t.Errorf("routing resources:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackRoutingErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`ActionDispatch::Routing::RouteSet.new.draw`, "ArgumentError"},
		{`ActionDispatch::Routing::RouteSet.new.draw { get "x(", to: "a#b" }`, "ArgumentError"},
		{`ActionDispatch::Routing::RouteSet.new.path("nope")`, "ActionController::UrlGenerationError"},
		{`r=ActionDispatch::Routing::RouteSet.new; r.draw { get "p/:id", to: "a#b", as: "p" }; r.path("p")`, "ActionController::UrlGenerationError"},
		{`ActionDispatch::Routing::RouteSet.new.path_args("nope")`, "ActionController::UrlGenerationError"},
		{`r=ActionDispatch::Routing::RouteSet.new; r.draw { get "p/:id", to: "a#b", as: "p" }; r.path_args("p", 1, 2)`, "ActionController::UrlGenerationError"},
		{`ActionDispatch::Routing::RouteSet.new.path_args`, "ArgumentError"},
		{`ActionDispatch::Routing::RouteSet.new.url_for(controller: "x", action: "y")`, "ActionController::UrlGenerationError"},
		{`ActionDispatch::Routing::RouteSet.new.draw { namespace(:a) }`, "ArgumentError"},
	}
	for _, c := range cases {
		if got := acRunErr(t, c.src); got != c.want {
			t.Errorf("err(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestActionPackRoutingDefaultsAndScopeExtras(t *testing.T) {
	// defaults:, constraints:, controller:/action:, on:, scope defaults/constraints/as.
	src := `
routes = ActionDispatch::Routing::RouteSet.new
routes.draw do
  get "widgets", controller: "widgets", action: "index", defaults: { fmt: "json" }
  get "items/:id", to: "items#show", constraints: { id: "[0-9]+" }
  scope path: "s", as: "s", constraints: { x: "1" }, defaults: { d: "2" } do
    get "inner", to: "inner#go"
  end
end
r = routes.recognize("GET", "/widgets")
puts r["controller"]
puts r["fmt"]
puts routes.recognize("GET", "/items/5")["action"]
puts routes.recognize("GET", "/s/inner")["controller"]
`
	got := acRun(t, src)
	want := "widgets\njson\nshow\ninner"
	if got != want {
		t.Errorf("routing defaults:\n got=%q\nwant=%q", got, want)
	}
}

// --- parameters ------------------------------------------------------------

func TestActionPackParameters(t *testing.T) {
	src := `
p = ActionController::Parameters.new(name: "Bob", age: 30, tags: ["a","b"], user: { email: "e", role: "r" })
puts p.class
puts p.permitted?
puts p.key?(:name)
puts p.has_key?(:missing)
puts p.keys.sort.join(",")
permitted = p.permit(:name, tags: [], user: [:email])
puts permitted.permitted?
puts permitted[:name]
puts permitted[:missing].inspect
puts permitted.to_h["name"]
puts p.to_unsafe_h["age"]
puts p.require(:name)
nested = p.require(:user)
puts nested.class
puts nested[:email]
vals = p.require([:name, :age])
puts vals.join("/")
merged = p.merge(ActionController::Parameters.new(name: "Alice"))
puts merged[:name]
`
	got := acRun(t, src)
	want := "ActionController::Parameters\nfalse\ntrue\nfalse\nage,name,tags,user\ntrue\nBob\nnil\nBob\n30\nBob\nActionController::Parameters\ne\nBob/30\nAlice"
	if got != want {
		t.Errorf("parameters:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackParametersErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`ActionController::Parameters.new(a: 1).require(:missing)`, "ActionController::ParameterMissing"},
		{`ActionController::Parameters.new(a: 1).require([:a, :missing])`, "ActionController::ParameterMissing"},
		{`ActionController::Parameters.new(a: 1).to_h`, "ActionController::UnfilteredParameters"},
		{`ActionController::Parameters.new(a: 1).merge(42)`, "TypeError"},
	}
	for _, c := range cases {
		if got := acRunErr(t, c.src); got != c.want {
			t.Errorf("err(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

// --- dispatch (Request/Response) -------------------------------------------

func TestActionPackDispatchRequest(t *testing.T) {
	src := `
req = ActionDispatch::Request.new("REQUEST_METHOD" => "GET", "PATH_INFO" => "/posts", "QUERY_STRING" => "id=5&q=hi")
req.set_path_parameters(controller: "posts", action: "show")
puts req.class
puts req.request_method
puts req.path
puts req.controller_name
puts req.action_name
puts req.format
puts req.params[:id]
puts req.query_parameters["q"]
puts req.request_parameters.length
puts req.path_parameters["action"]
`
	got := acRun(t, src)
	want := "ActionDispatch::Request\nGET\n/posts\nposts\nshow\nhtml\n5\nhi\n0\nshow"
	if got != want {
		t.Errorf("dispatch request:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackDispatchRequestErrors(t *testing.T) {
	bad := `ActionDispatch::Request.new("QUERY_STRING" => "a=%zz")`
	for _, m := range []string{".params", ".query_parameters"} {
		if got := acRunErr(t, bad+m); got != "ArgumentError" {
			t.Errorf("err(%s) = %q, want ArgumentError", m, got)
		}
	}
}

// TestActionPackRequestParamsError covers the request_parameters (POST body)
// parse-error branch, which needs a real rack.Input the Ruby env cannot express.
func TestActionPackRequestParamsError(t *testing.T) {
	vm := New(nil)
	env := rack.Env{
		"REQUEST_METHOD": "POST",
		"CONTENT_TYPE":   "application/x-www-form-urlencoded",
		"rack.input":     &acBadInput{data: []byte("a=%zz")},
	}
	req := &ACRequest{r: dispatch.NewRequest(env), cls: vm.cACRequest}
	var class string
	func() {
		defer func() {
			if r := recover(); r != nil {
				if re, ok := r.(RubyError); ok {
					class = re.Class
				}
			}
		}()
		vm.send(req, "request_parameters", nil, nil)
	}()
	if class != "ArgumentError" {
		t.Errorf("request_parameters bad body raised %q, want ArgumentError", class)
	}
}

// acBadInput is a one-shot rack.Input returning a malformed urlencoded body.
type acBadInput struct {
	data []byte
	done bool
}

func (b *acBadInput) Read(int) ([]byte, error) {
	if b.done {
		return nil, nil
	}
	b.done = true
	return b.data, nil
}

func TestActionPackDispatchResponse(t *testing.T) {
	src := `
resp = ActionDispatch::Response.new
puts resp.class
puts resp.status
resp.status = 201
resp.write("hello ")
resp.write("world")
puts resp.status
puts resp.body
resp["X-Test"] = "yes"
resp.content_type = "text/plain"
puts resp["X-Test"]
puts resp.headers["content-type"]
resp2 = ActionDispatch::Response.new
resp2.redirect("/here", 303)
puts resp2.status
puts resp2.headers["location"]
`
	got := acRun(t, src)
	want := "ActionDispatch::Response\n200\n201\nhello world\nyes\ntext/plain\n303\n/here"
	if got != want {
		t.Errorf("dispatch response:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackDispatchResponseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`r=ActionDispatch::Response.new; r["only-key"]=nil; r.send(:[]=, "k")`, "ArgumentError"},
		{`ActionDispatch::Response.new.redirect`, "ArgumentError"},
	}
	for _, c := range cases {
		if got := acRunErr(t, c.src); got != c.want {
			t.Errorf("err(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

// --- controller ------------------------------------------------------------

func TestActionPackControllerBasic(t *testing.T) {
	src := `
class PostsController < ActionController::Base
  def index
    render plain: "all posts"
  end
  def show
    render plain: "post #{params[:id]}", status: 200, content_type: "text/plain"
  end
  def by_name
    render "posts/named"
  end
  def created
    render json: { ok: true }, status: 201, layout: "app", action: "created"
  end
  def go_away
    redirect_to "/login"
  end
  def go_perm
    redirect_to "/new", status: 301
  end
  def gone
    head 410
  end
  def whoami
    render plain: "#{action_name}/#{performed?}/#{request.request_method}/#{response.status}"
  end
end
puts PostsController.dispatch(:index)[2][0]
puts PostsController.dispatch(:show, "QUERY_STRING" => "id=7")[2][0]
puts PostsController.dispatch(:go_away)[0]
puts PostsController.dispatch(:go_away, {})[1]["location"]
puts PostsController.dispatch(:go_perm)[0]
puts PostsController.dispatch(:gone)[0]
puts PostsController.dispatch(:whoami, "REQUEST_METHOD" => "POST")[2][0]
puts PostsController.dispatch(:created)[0]
puts PostsController.dispatch(:by_name)[0]
`
	got := acRun(t, src)
	want := "all posts\npost 7\n302\n/login\n301\n410\nwhoami/false/POST/200\n201\n200"
	if got != want {
		t.Errorf("controller basic:\n got=%q\nwant=%q", got, want)
	}
}

// TestActionPackControllerEdgeCases covers the remaining seam branches: an
// around filter whose inner action raises (re-raised at the yield point), a
// non-Ruby panic (throw) surfacing through the body recover, and a dispatch
// whose request params fail to parse.
func TestActionPackControllerEdgeCases(t *testing.T) {
	aroundRaise := `
class AroundRaiseController < ActionController::Base
  around_action :wrap
  def wrap; yield; end
  def boom; raise "inner"; end
end
AroundRaiseController.dispatch(:boom)
`
	if got := acRunErr(t, aroundRaise); got != "RuntimeError" {
		t.Errorf("around raise = %q, want RuntimeError", got)
	}

	throwSrc := `
class ThrowController < ActionController::Base
  def t; throw :nope; end
end
ThrowController.dispatch(:t)
`
	if got := acRunErr(t, throwSrc); got == "" {
		t.Error("throw should surface an uncaught error")
	}

	badQuery := `
class QController < ActionController::Base
  def index; render plain: "x"; end
end
QController.dispatch(:index, "QUERY_STRING" => "a=%zz")
`
	if got := acRunErr(t, badQuery); got != "ArgumentError" {
		t.Errorf("dispatch bad query = %q, want ArgumentError", got)
	}
}

// TestActionPackRoutingExtras covers the routing-helper branches not hit by the
// main DSL tests: a verb with no options, `on:`, a positional/empty root target,
// a bare scope, and path_args with a trailing options Hash.
func TestActionPackRoutingExtras(t *testing.T) {
	src := `
r1 = ActionDispatch::Routing::RouteSet.new
r1.draw do
  get "healthz"
  get to: "home#dashboard"
  resources :things do
    get "activate", to: "things#activate", on: :member
  end
  root "welcome#index"
  scope do
    get "bare", to: "bare#go"
  end
end
puts r1.recognize("GET", "/things/1/activate")["action"]
puts r1.recognize("GET", "/")["controller"]
puts r1.recognize("GET", "/bare")["controller"]
puts r1.path_args("thing", 5, format: "json")

r2 = ActionDispatch::Routing::RouteSet.new
r2.draw { root }
puts r2.recognize("GET", "/")["controller"].inspect
`
	got := acRun(t, src)
	want := "activate\nhome\nbare\n/things/5.json\n\"\""
	if got != want {
		t.Errorf("routing extras:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackControllerFilters(t *testing.T) {
	src := `
class AppController < ActionController::Base
  before_action :log_start
  def log_start
    @trace = "start"
  end
end
class GuardedController < AppController
  before_action :require_login, only: [:secret]
  before_action { @trace = "#{@trace}|blk" }
  after_action :log_end, except: [:secret]
  around_action :timing
  before_action :block_it, if: :blocked?, unless: -> { false }

  def open
    render plain: "#{@trace}|open|#{@timing}"
  end
  def secret
    render plain: "secret"
  end
  def blocked
    render plain: "should not reach"
  end

  def require_login
    render plain: "denied", status: 403
  end
  def log_end
    @trace = "#{@trace}|end"
  end
  def timing
    @timing = "T0"
    yield
    @timing = "T1"
  end
  def blocked?
    action_name == "blocked"
  end
  def block_it
    render plain: "blocked!", status: 400
  end
end
puts GuardedController.dispatch(:open)[2][0]
puts GuardedController.dispatch(:secret)[0]
puts GuardedController.dispatch(:secret)[2][0]
puts GuardedController.dispatch(:blocked)[0]
puts GuardedController.dispatch(:blocked)[2][0]
`
	got := acRun(t, src)
	// open: before log_start ("start") + block ("|blk"), around sets @timing T0 before
	// action runs (render reads it), timing filter runs, no login guard (only :secret),
	// no block_it (blocked? false for "open").
	want := "start|blk|open|T0\n403\ndenied\n400\nblocked!"
	if got != want {
		t.Errorf("controller filters:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackControllerRescue(t *testing.T) {
	src := `
class RescuingController < ActionController::Base
  rescue_from ArgumentError do |e|
    render plain: "rescued: #{e.message}", status: 422
  end
  rescue_from RuntimeError, with: :handle_runtime
  rescue_from AbstractController::ActionNotFound do |e|
    render plain: "no action", status: 404
  end

  def boom
    raise ArgumentError, "bad arg"
  end
  def kaboom
    raise "runtime issue"
  end
  def handle_runtime(e)
    render plain: "runtime: #{e.message}", status: 500
  end
end
puts RescuingController.dispatch(:boom)[0]
puts RescuingController.dispatch(:boom)[2][0]
puts RescuingController.dispatch(:kaboom)[2][0]
puts RescuingController.dispatch(:no_such_action)[0]
puts RescuingController.dispatch(:no_such_action)[2][0]
`
	got := acRun(t, src)
	want := "422\nrescued: bad arg\nruntime: runtime issue\n404\nno action"
	if got != want {
		t.Errorf("controller rescue:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackControllerRescueMiss(t *testing.T) {
	// rescue_from that does not match the raised error -> propagates to Ruby.
	src := `
class NarrowController < ActionController::Base
  rescue_from ArgumentError do |e| render plain: "nope" end
  def boom; raise "not an ArgumentError"; end
end
NarrowController.dispatch(:boom)
`
	if got := acRunErr(t, src); got != "RuntimeError" {
		t.Errorf("rescue miss = %q, want RuntimeError", got)
	}
	// A truly unmapped action with no rescue -> AbstractController::ActionNotFound.
	src2 := `
class BareController < ActionController::Base; end
BareController.dispatch(:ghost)
`
	if got := acRunErr(t, src2); got != "AbstractController::ActionNotFound" {
		t.Errorf("bare missing action = %q, want AbstractController::ActionNotFound", got)
	}
}

func TestActionPackControllerViewContext(t *testing.T) {
	src := `
class StringView
  def render(opts)
    "view[#{opts[:template]}/#{opts[:action]}/#{opts[:plain]}/#{opts[:status]}/#{opts[:layout]}]"
  end
end
class IntView
  def render(opts); 4242; end
end
class ViewedController < ActionController::Base
  view_context StringView.new
  def show; render template: "posts/show", status: 200, layout: "main"; end
end
class IntViewedController < ActionController::Base
  view_context IntView.new
  def show; render plain: "ignored"; end
end
puts ViewedController.dispatch(:show)[2][0]
puts IntViewedController.dispatch(:show)[2][0]
`
	got := acRun(t, src)
	want := "view[posts/show///200/main]\n4242"
	if got != want {
		t.Errorf("controller view context:\n got=%q\nwant=%q", got, want)
	}
}

func TestActionPackControllerDoubleRender(t *testing.T) {
	src := `
class DoubleController < ActionController::Base
  def twice
    render plain: "one"
    render plain: "two"
  end
  def render_then_redirect
    render plain: "x"
    redirect_to "/y"
  end
  def double_head
    head 200
    head 201
  end
end
DoubleController.dispatch(:twice)
`
	if got := acRunErr(t, src); got != "AbstractController::DoubleRenderError" {
		t.Errorf("double render = %q", got)
	}
	for _, a := range []string{":render_then_redirect", ":double_head"} {
		s := `
class DoubleController < ActionController::Base
  def render_then_redirect; render plain: "x"; redirect_to "/y"; end
  def double_head; head 200; head 201; end
end
DoubleController.dispatch(` + a + `)`
		if got := acRunErr(t, s); got != "AbstractController::DoubleRenderError" {
			t.Errorf("double(%s) = %q", a, got)
		}
	}
}

func TestActionPackControllerNoArgErrors(t *testing.T) {
	src := `
class ArgController < ActionController::Base
  def bad; redirect_to; end
end
ArgController.dispatch(:bad)
`
	if got := acRunErr(t, src); got != "ArgumentError" {
		t.Errorf("redirect_to no arg = %q", got)
	}
}

func TestActionPackControllerRescueWithBlockException(t *testing.T) {
	// A rescue_from whose exception has a plain message; covers acErrValue's
	// acRubyErr path and the with:-method handler branch already; here we cover a
	// controller whose action defines its own render context via inheritance.
	src := `
class BaseWithRescue < ActionController::Base
  rescue_from StandardError, with: :on_error
  def on_error(e); render plain: "handled #{e.class}"; end
end
class ChildController < BaseWithRescue
  def boom; raise ArgumentError, "x"; end
end
puts ChildController.dispatch(:boom)[2][0]
`
	got := acRun(t, src)
	want := "handled ArgumentError"
	if got != want {
		t.Errorf("inherited rescue:\n got=%q\nwant=%q", got, want)
	}
}

// TestActionPackRescueFromArgError covers rescue_from with no class argument.
func TestActionPackRescueFromArgError(t *testing.T) {
	src := `
class NoClassController < ActionController::Base
  rescue_from with: :x
end
`
	if got := acRunErr(t, src); got != "ArgumentError" {
		t.Errorf("rescue_from no class = %q, want ArgumentError", got)
	}
}
