// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	actionmailer "github.com/go-ruby-actionmailer/actionmailer"
	activejob "github.com/go-ruby-activejob/activejob"
	activestorage "github.com/go-ruby-activestorage/activestorage"
	inflector "github.com/go-ruby-activesupport/activesupport/inflector"
	async "github.com/go-ruby-async/async"
	capistrano "github.com/go-ruby-capistrano/capistrano"
	facter "github.com/go-ruby-facter/facter"
	factorybot "github.com/go-ruby-factory-bot/factory-bot"
	fastgettext "github.com/go-ruby-fast-gettext/fast-gettext"
	i18n "github.com/go-ruby-i18n/i18n"
	money "github.com/go-ruby-money/money"
	rake "github.com/go-ruby-rake/rake"
	sinatra "github.com/go-ruby-sinatra/sinatra"
	vcr "github.com/go-ruby-vcr/vcr"
)

// gemState is the per-VM state of the gem bindings, embedded in VM. These
// fields used to be declared in VM itself, which made vm.go import fourteen
// gem packages, and an imported package's init is linked whether or not
// anything calls into it. They moved here unchanged when the rbgo_core build
// stopped compiling the gem bindings; gems_core.go declares an empty gemState.
type gemState struct {
	arAdapter *arSQLiteAdapter               // the ActiveRecord::Base connection (require "active_record"), backed by go-ruby-sqlite3; nil until establish_connection
	arModels  map[*RClass]*ActiveRecordModel // the *ActiveRecordModel lazily built for each `class … < ActiveRecord::Base` subclass

	moneyBank     *money.VariableExchange // the process-wide default exchange bank for Money (require "money")
	i18nInst      *i18n.I18n              // the process-wide I18n instance (require "i18n"), backed by go-ruby-i18n
	asInflections *inflector.Inflections  // the ActiveSupport::Inflector inflection ruleset (require "active_support"), backed by go-ruby-activesupport; a per-VM clone of the gem's English defaults
	otelProvider  *OTelTracerProvider     // the process-wide OpenTelemetry.tracer_provider (require "opentelemetry"), backed by go-ruby-opentelemetry

	sinatraDefs     map[*RClass]*sinatraDef          // per-Sinatra::Base-subclass route/filter/handler declarations
	sinatraCtxCache map[*sinatra.Context]*SinatraCtx // per-request handler self, shared across before/route/after so @ivars persist
	sinatraSession  *sinatraSessionState             // per-dispatch cookie session (enable :sessions); the `session` helper returns its live Hash, saved back into a Set-Cookie

	curAsyncTask *async.Task                // the task whose Ruby body is currently running (backs Async::Task.current and the caller passed to blocking async ops)
	asyncTasks   map[*async.Task]*AsyncTask // wrapper cache so one *async.Task always maps to one Async::Task object (Ruby #equal? identity), cleared when the root reactor finishes

	omniAuthConfig *OmniAuthConfig                // the shared OmniAuth.config (test_mode / mock_auth / path_prefix)
	amThunks       map[*RClass][]amThunk          // ActiveModel::Validations DSL registrations per class (require "active_model"); replayed onto a fresh activemodel.Validations at #valid? time so subclasses inherit ancestors' validators
	fidConfigs     map[*RClass]*fidState          // FriendlyId: per-model slug config, reference slug store and in-process record cache (require "friendly_id"), installed by the friendly_id macro
	aasmSpecs      map[*RClass][]*aasmMachineSpec // AASM: per-class state-machine definitions from `aasm do … end` (require "aasm"); replayed to build a bound aasm.Instance per Ruby object, walked over ancestors so a subclass inherits its machines
	// paperTrail holds the model-versioning state (require "paper_trail"): the
	// shared version store, per-class has_paper_trail config, request whodunnit
	// and the injectable clock. Created once by registerPaperTrail.
	paperTrail *paperTrailState

	ajBases        map[*RClass]*activejob.Base        // ActiveJob: library job class built per `class … < ActiveJob::Base` subclass (class-dispatch seam)
	ajJobOf        map[*RObject]*activejob.Job        // ActiveJob: Ruby job instance -> backing library *Job
	ajInstOf       map[*activejob.Job]*RObject        // ActiveJob: library *Job -> Ruby job instance
	ajTestAdapters map[*RClass]*activejob.TestAdapter // ActiveJob: per-class :test adapter
	ajArgs         *activejob.Arguments               // ActiveJob: module-level ActiveJob::Arguments serializer (GlobalID seam wired in registerActiveJob)

	amDefs      map[*RClass]*amDef                // ActionMailer: per-mailer-class default/delivery/hook declarations, merged down the chain at delivery time
	sftpClasses *sftpClassSet                     // Net::SFTP: the wrapper classes stamped on returned Attributes / Name / File values (require "net/sftp")
	amMailerOf  map[*RObject]*actionmailer.Mailer // ActionMailer: mailer instance -> library *Mailer of its running action (the mail/attachments/headers seam)
	asConfig    *activestorage.Config             // ActiveStorage process config (require "active_storage"); nil until first use, then a deterministic in-process config (MemStore + DiskService temp dir)

	railtieSeams     map[any]*railtieSeam         // per-railtie/engine/app deferred initializer blocks, keyed by the library ctx object; run inline by the RunInitializer seam during Application#initialize!
	railsApp         *RailsAppVal                 // the Ruby Application registered via Rails.application= (the rails meta-gem's top-level app); nil before boot
	deviseConfig     *DeviseConfig                // the shared Devise.config the DatabaseAuthenticatable Warden strategy authenticates against
	hanamiActionDefs map[*RClass]*hanamiActionDef // per-Hanami::Action-subclass before/after/handle_exception/accept/config declarations
	acControllerDefs map[*RClass]*acControllerDef // per-ActionController::Base-subclass before/after/around/rescue_from/view-context declarations

	factoryBotReg *factorybot.Registry  // the per-VM factory_bot registry (require "factory_bot"): FactoryBot.define/build/create map onto it; the Build/Persist/Block seams are wired to this VM's object model
	facterFacter  *facter.Facter        // the per-VM Facter adapter (require "facter"), from go-ruby-facter over go-facter: Facter.value/[]/fact/add/to_hash read it; custom facts (Facter.add … setcode) never leak across interpreters
	fastGettext   *fastgettext.Instance // the per-VM FastGettext instance (require "fast_gettext"), from go-ruby-fast-gettext: text domains, current locale/text_domain and the translation cache never leak across interpreters
	fakerInst     *fakerState           // Faker instance + its seed source (Faker::Config.random)

	// rakeApp is the per-VM Rake task registry (Rake.application), created by
	// registerRake at bootstrap. The top-level task/file/namespace/desc DSL and
	// the Rake::Task[] lookup all resolve against it. GVL-guarded.
	rakeApp *rake.Application
	// capApp is the per-VM Capistrano DSL facade (the deploy `env` + task graph),
	// created lazily by installCapistrano on the first `require "capistrano"`. The
	// top-level set/fetch/role/server/task/before/after/invoke/on DSL resolves
	// against it. capBackend is its injected in-process command backend (the
	// library's FakeBackend), so execute/capture/test are recorded and never touch
	// a real host — keeping every run hermetic. Both are nil until required and are
	// GVL-guarded thereafter.
	capApp     *capistrano.Application
	capBackend *capistrano.FakeBackend
	// relineState holds the mutable Reline module configuration (require
	// "reline"): the shared history backing Reline::HISTORY, the injected
	// input/output IO seams, the Ruby completion proc, and the editing mode. It
	// is created once by registerReline and mutated by the module's setters.
	relineState *relineState
	// simpleCov holds the mutable SimpleCov module state (require "simplecov"):
	// the go-ruby-simplecov result-engine configuration (filters, groups,
	// thresholds), the accumulated per-file raw line-coverage map fed through the
	// deferred VM line-coverage seam (SimpleCov.add_coverage), and the active
	// formatter. It is created once by registerSimpleCov and reset by
	// SimpleCov.start.
	simpleCov *simpleCovState

	// vcrConfig is the VCR module's configuration + cassette store (require "vcr"),
	// backed by go-ruby-vcr: it holds cassette_library_dir, default_record_mode, the
	// filesystem seam (OSFS by default) and the recorded_at clock. Created by
	// registerVCR. vcrCassette is the cassette of the currently-active
	// VCR.use_cassette block (nil outside one); while it is non-nil, bound Net::HTTP
	// requests route through it (replay a recorded interaction or record via the
	// real transport) instead of dialing directly. vcrTestDoer, when non-nil,
	// substitutes the record doer with a canned one so the record/replay state
	// machine is drivable in-process with no network. All three are GVL-guarded.
	vcrConfig   *vcr.VCR
	vcrCassette *vcr.Cassette
	vcrTestDoer func(vcr.Request) (vcr.Response, error)
}
