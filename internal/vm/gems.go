// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

// This file holds the registrations of everything the rbgo_core build leaves
// out (core.go says what core is), grouped in the runs bootstrap and
// NewWithStderr call them in. Each group is called from exactly the place its
// calls used to stand, so on the default build the order of every registration
// is the order it always was. gems_core.go gives the rbgo_core build an empty
// body for each group.
//
// Why a separate file and not `if !coreBuild` around the calls: not calling a
// gem's register function drops almost nothing from the binary. The linker
// keeps the init of every package a compiled file imports, and everything that
// init reaches; measured with wasmsize, a core build that only skipped the calls
// still carried google.golang.org/protobuf (2.5 MB), go-scss (2.0 MB), go-rouge
// (1.1 MB) and the rest. So the files that import a gem carry
// `//go:build !rbgo_core` too, and the calls into them have to live in a file
// that carries it, which is this one.

package vm

// registerImageGem is Image, over go-images (bootstrap, after NDArray).
func (vm *VM) registerImageGem() {
	vm.registerImage()
}

// registerNetworkGems is the network stack and the gems over it (bootstrap,
// after GC).
func (vm *VM) registerNetworkGems() {
	vm.registerOpenSSL()          // OpenSSL (real digest/HMAC/random + PKI/TLS shell); needs StandardError
	vm.registerSocket()           // TCPSocket/TCPServer (net) + OpenSSL::SSL::SSLSocket (crypto/tls); after registerOpenSSL (upgrades its SSL shell)
	vm.registerNetHTTP()          // net/http + net/https loadable shell; needs StandardError
	vm.registerNetHTTPTransport() // real Net::HTTP over the socket transport; after registerNetHTTP + registerSocket
	vm.registerWebMock()          // WebMock stub registry (require "webmock") intercepting the bound Net::HTTP transport; after registerNetHTTP + registerNetHTTPTransport
	vm.registerVCR()              // VCR record/replay of the bound Net::HTTP via cassettes (require "vcr"), backed by go-ruby-vcr; after registerNetHTTP + registerNetHTTPTransport (it routes the real transport)
	vm.registerNetPOP()           // Net::POP3/Net::POPMail (require "net/pop"), backed by go-ruby-net-pop codec; socket = injected IO seam; after registerNetHTTP (Net module) + registerSocket/registerOpenSSL
	vm.registerNetSFTP()          // Net::SFTP client (require "net/sftp"), backed by go-ruby-net-sftp codec; SSH channel = injected IO seam; nests under Net, after registerNetHTTP
	vm.registerNetFTP()           // real Net::FTP over the socket transport; after registerNetHTTP (Net module) + registerSocket/registerOpenSSL
	vm.registerNetLDAP()          // Net::LDAP client (require "net/ldap"/"net-ldap"/"ldap"), backed by go-ruby-ldap over github.com/go-ldap/ldap/v3; nests under Net, after registerNetHTTP (Net module); needs StandardError for Net::LDAP::Error
	vm.registerActiveLdap()       // ActiveLdap ORM: Connection/Model/Record + ldap_mapping/find/save/to_ldif (require "active_ldap"), backed by go-ruby-activeldap; Directory seam is the library MockDirectory or a Net::LDAP-shaped object (go-ruby-ldap); after registerNetLDAP; needs StandardError for ActiveLdap::Error
	vm.registerResolv()           // Resolv (real IPv4/IPv6 parse; DNS sockets stubbed); needs StandardError
}

// registerTimecopGem is Timecop (bootstrap, after Timeout). The clock it drives,
// vm.clock, is core: Time.now reads it on every build.
func (vm *VM) registerTimecopGem() {
	vm.registerTimecop() // Timecop module (require "timecop"), backed by go-ruby-timecop; drives vm.clock behind Time.now/Date.today/DateTime.now
}

// registerThirdPartyGems is the third-party gems, and yaml (bootstrap, after
// JSON).
func (vm *VM) registerThirdPartyGems() {
	vm.registerMultiJson()      // MultiJson module (require "multi_json"), backed by go-ruby-multi-json for the adapter registry + errors; parse/generate route through rbgo's ordered JSON so key order + symbol keys survive; needs StandardError + ArgumentError
	vm.registerYAML()           // YAML/Psych loadable shell; needs StandardError
	vm.registerBCrypt()         // BCrypt (require "bcrypt"), backed by go-ruby-bcrypt; needs StandardError + String
	vm.registerJWT()            // JWT (require "jwt"), backed by go-ruby-jwt; needs StandardError
	vm.registerRbNaCl()         // RbNaCl (require "rbnacl"), backed by go-ruby-sodium; needs StandardError
	vm.registerAge()            // Age (require "age"), backed by go-ruby-age; needs StandardError
	vm.registerPrawn()          // Prawn::Document (require "prawn"), backed by go-ruby-prawn (fpdf); needs StandardError for Prawn::Errors
	vm.registerBleve()          // Bleve module (require "bleve"), backed by go-ruby-bleve; needs StandardError (Bleve::Error)
	vm.registerFaraday()        // Faraday HTTP client (require "faraday"), backed by go-ruby-faraday; needs StandardError for Faraday::Error
	vm.registerPuma()           // Puma module (require "puma"), backed by go-ruby-puma; threaded Rack web server over net/http; needs StandardError (Puma::Error) + StringIO (rack.input)
	vm.registerBolt()           // Bolt::DB/Tx/Bucket/Cursor (require "bolt"), backed by go-ruby-bbolt; needs StandardError for Bolt::Error
	vm.registerSimpleCov()      // SimpleCov module + SimpleCov::Result/SourceFile/Formatter (require "simplecov"), backed by go-ruby-simplecov result engine; live line-coverage collection is a deferred VM feature (coverage supplied via SimpleCov.add_coverage); needs Object + Time
	vm.registerSAML()           // SAML / OneLogin::RubySaml (require "saml"/"ruby-saml"), backed by go-ruby-saml; needs StandardError for SAML::Error
	vm.registerWebAuthn()       // WebAuthn module (require "webauthn"), backed by go-ruby-webauthn; needs StandardError (WebAuthn::Error)
	vm.registerACME()           // Acme::Client / Order / Authorization / Challenge / CertificateRequest (require "acme" / "acme/client"), backed by go-ruby-acme (x/crypto/acme); transport is a host seam; needs StandardError for the Acme::Error tree
	vm.registerGRPC()           // GRPC module (require "grpc"), backed by go-ruby-grpc over google.golang.org/grpc; in-process bufconn transport; needs StandardError (GRPC::Error)
	vm.registerNATS()           // NATS module (require "nats"), backed by go-ruby-nats over nats.go; needs StandardError for the NATS::Error tree
	vm.registerKafka()          // Kafka module (require "kafka"), backed by go-ruby-kafka (twmb/franz-go); needs StandardError for Kafka::Error
	vm.registerEtcd()           // Etcd/Etcdv3 client (require "etcd"/"etcdv3"), backed by go-ruby-etcd over go.etcd.io/etcd/client/v3; needs StandardError for Etcd::Error
	vm.registerVault()          // Vault/OpenBao client (require "vault"/"openbao"), backed by go-ruby-openbao; transport wired to the bound Net::HTTP; needs StandardError for Vault::VaultError; after registerNetHTTP + registerNetHTTPTransport
	vm.registerRolify()         // rolify / resourcify class macros (require "rolify"), backed by go-ruby-rolify; role store seam = the reference MemoryStore
	vm.registerMySQL()          // Mysql2::Client/Result/Statement (require "mysql2" / "mysql"), backed by go-ruby-mysql over go-sql-driver; needs Enumerable for Result and StandardError for Mysql2::Error
	vm.registerMongo()          // Mongo::Client/Database/Collection/Cursor + BSON::ObjectId (require "mongo"), backed by go-ruby-mongodb; needs StandardError for Mongo::Error
	vm.registerParquet()        // Parquet::ArrowFileReader/ArrowFileWriter (require "parquet"), backed by go-ruby-parquet; needs StandardError for Parquet::Error
	vm.registerHTTParty()       // HTTParty HTTP client (require "httparty"), backed by go-ruby-httparty; needs StandardError for HTTParty::Error
	vm.registerConnectionPool() // ConnectionPool + ConnectionPool::Wrapper (require "connection_pool"), backed by go-ruby-connection-pool; needs RuntimeError + Timeout::Error
	vm.registerErubi()          // Erubi::Engine / CaptureEndEngine + Erubi.h (require "erubi"), template->Ruby-source compiler backed by go-ruby-erubi
	vm.registerReline()         // Reline module + Reline::HISTORY (require "reline"), line-editor backed by go-ruby-reline; the terminal I/O is wired to rbgo IO objects via Reline.input=/output=
	vm.registerHTTPrb()         // HTTP module — chainable http.rb client (require "http"), backed by go-ruby-http; needs StandardError for HTTP::Error
	vm.registerExcon()          // Excon module — persistent HTTP client (require "excon"), backed by go-ruby-excon; needs StandardError for Excon::Error
	vm.registerTyphoeus()       // Typhoeus module — parallel HTTP client + Hydra (require "typhoeus"), backed by go-ruby-typhoeus; net/http+goroutines
	vm.registerPundit()         // Pundit mixin (require "pundit"), backed by go-ruby-pundit; policy dispatch + safe_constantize are rbgo seams
	vm.registerFriendlyId()     // FriendlyId slug macro (require "friendly_id"), backed by go-ruby-friendly-id; base-attr read / uniqueness / history are rbgo seams
	vm.registerCanCanCan()      // CanCan::Ability mixin (require "cancancan" / "cancan"), backed by go-ruby-cancancan; attr read + block eval are rbgo seams
	vm.registerMsgpack()        // MessagePack module (go-ruby-msgpack backend); needs StandardError for MessagePack::Error
	vm.registerTOML()           // TOML/TomlRB module (go-ruby-toml backend); needs StandardError for TomlRB::ParseError
	vm.registerTZInfo()         // TZInfo module (go-ruby-tzinfo backend); needs StandardError for TZInfo::InvalidTimezoneIdentifier
	vm.registerChronic()        // Chronic module (go-ruby-chronic backend); needs StandardError
	vm.registerMoney()          // Money module (go-ruby-money backend); needs StandardError for Money::Currency::UnknownCurrency
	vm.registerAddressable()    // Addressable module (go-ruby-addressable backend); needs StandardError
	vm.registerPublicSuffix()   // PublicSuffix module (go-ruby-public-suffix backend); needs StandardError for PublicSuffix::Error tree
	vm.registerMIMETypes()      // MIME::Types module (go-ruby-mime-types backend)
	vm.registerCommonmark()     // Commonmark.render_html / String#to_html (require "commonmark"), backed by go-commonmark
	vm.registerMustache()       // Mustache.render + Mustache view class (require "mustache"); needs StandardError for Mustache::Error
	vm.registerJbuilder()       // Jbuilder.encode / json.<name> DSL (require "jbuilder"), backed by go-ruby-jbuilder
	vm.registerBuilder()        // Builder::XmlMarkup (require "builder"), backed by go-ruby-builder
	vm.registerSQLite3()        // SQLite3::Database/Statement (require "sqlite3"), backed by go-ruby-sqlite3 (modernc); needs StandardError for SQLite3::Exception
	vm.registerPG()             // PG::Connection/Result (require "pg"), backed by go-ruby-pg v3 protocol; socket = injected IO seam; needs StandardError for PG::Error
	vm.registerNetIMAP()        // Net::IMAP (require "net/imap"), backed by go-ruby-net-imap builder+parser; socket = injected IO seam; after socket/openssl so a TCPSocket/SSLSocket can be the TLS seam
	vm.registerNetSMTP()        // Net::SMTP (require "net/smtp"), backed by go-ruby-net-smtp codec over rbgo's real socket/TLS transport; after registerSocket/registerNetHTTP (Net module + transport)
	vm.registerSidekiq()        // Sidekiq::Client/Job(Worker) + module (require "sidekiq"), backed by go-ruby-sidekiq over go-redis/v9; Redis client dialled from Sidekiq.redis/ENV; perform body is Ruby seam; needs StandardError for Sidekiq::Error
	vm.registerResque()         // Resque module + Job/Worker/Failure (require "resque"), backed by go-ruby-resque over go-redis/v9; @queue resolver + self.perform are Ruby seams; needs StandardError for Resque::Error
	vm.registerSequel()         // Sequel query builder + Database (require "sequel"), backed by go-ruby-sequel; executor seam wired to SQLite3::Database (real execution)
	vm.registerNokogiri()       // Nokogiri::HTML/XML -> Document/Node/NodeSet (require "nokogiri"), backed by go-nokogiri; needs StandardError for Nokogiri::SyntaxError
	vm.registerNokogiriXSLT()   // Nokogiri::XSLT(str) -> Stylesheet#transform/apply_to (require "nokogiri"), backed by go-xslt over go-nokogiri; needs registerNokogiri first
	vm.registerRSpec()          // RSpec matcher + expect surface (require "rspec"), backed by go-ruby-rspec; needs Exception for ExpectationNotMetError
	vm.registerFactoryBot()     // FactoryBot.define/build/create/attributes_for/build_list/create_list/generate (require "factory_bot"), backed by go-ruby-factory-bot; the class-instantiation+attribute-assignment (BuildFunc), persistence (PersistFunc, save!) and dynamic-attribute/sequence/callback blocks are the rbgo object-model seams run INLINE under the GVL; needs StandardError for KeyError/ArgumentError raised on unknown/duplicate factories
	vm.registerRuboCop()        // RuboCop::Runner#inspect/autocorrect + Cop::Offense (require "rubocop"), backed by go-ruby-rubocop over go-ruby-parser; needs StandardError for RuboCop::Error
	vm.registerFacter()         // Facter.value/[]/fact/add/to_hash/clear (require "facter"), backed by go-facter; per-VM Collection; Facter.add … setcode block runs INLINE under the GVL when the custom fact resolves
	vm.registerHocon()          // Hocon.parse / Hocon::ConfigFactory + Config accessors (require "hocon"), backed by go-ruby-hocon over go-hocon; needs StandardError for Hocon::ConfigError
	vm.registerConfd()          // Confd.render(template, vars) (require "confd"), backed by go-ruby-confd over abtreece/confd; seeds confd's in-memory backend from a Ruby Hash ("/"-path keys, nested Hashes flattened) and runs confd's real text/template engine (getv/getvs/exists/base64Encode/json/…); needs StandardError for Confd::Error
	vm.registerFastGettext()    // FastGettext translation module (require "fast_gettext"), backed by go-ruby-fast-gettext; per-VM Instance holds domains/locale; _/n_/s_/p_ helpers
	vm.registerDeepMerge()      // DeepMerge.deep_merge!/deep_merge + Hash#deep_merge!/#deep_merge (require "deep_merge"), backed by go-ruby-deep-merge; Ruby key identity preserved; bad option combos raise DeepMerge::InvalidParameter
	vm.registerRack()           // Rack::Request/Response/Utils (require "rack" / "rack/utils"), backed by go-ruby-rack; deterministic env/query/escape, no socket
	vm.registerWEBrick()        // WEBrick::HTTPServer/HTTPRequest/HTTPResponse/HTTPServlet/HTTPStatus (require "webrick"), backed by go-ruby-webrick; deterministic parse/build/mount-dispatch, no socket; the mount_proc block / servlet do_* method is the rbgo seam run inline under the GVL; needs StandardError (WEBrick::HTTPStatus family)
	vm.registerGrape()          // Grape::Router/Validator/Formatter (require "grape"), backed by go-ruby-grape; endpoint-block exec + Rack env are host seams; needs StandardError for Grape::Exceptions
	vm.registerSinatra()        // Sinatra::Base class-DSL + #call Rack adapter (require "sinatra/base"), backed by go-ruby-sinatra over go-ruby-rack; route/filter block eval is the rbgo seam; needs Rack + StandardError (Sinatra::NotFound), so registered after registerRack
	vm.registerRoda()           // Roda class routing-tree DSL (route do |r| … end) + #call Rack adapter (require "roda"), backed by go-ruby-roda over go-ruby-rack; the route/matcher block eval is the rbgo seam, the routing tree is the library; needs Rack + StandardError (Roda::RodaError)
	vm.registerAsync()          // Async{} structured concurrency + Async::Task/Barrier/Semaphore/Condition/Notification/Queue/LimitedQueue/Waiter (require "async"), backed by go-ruby-async on its CoScheduler; the task-body block eval is the rbgo seam; needs StandardError (Async::Stop / Async::TimeoutError)
	vm.registerWarden()         // Warden::Manager/Proxy/Strategies (require "warden"), backed by go-ruby-warden over go-ruby-rack; strategy valid?/authenticate! bodies are the rbgo seam; needs Rack + StandardError (Warden::NotAuthenticated), so registered after registerRack
	vm.registerOmniAuth()       // OmniAuth::Builder/Strategy/AuthHash + OmniAuth.config (require "omniauth"), backed by go-ruby-omniauth over go-ruby-rack; provider request_phase/uid/info bodies are the rbgo seam; needs Rack + StandardError (OmniAuth::Error), so registered after registerRack
	vm.registerActiveModel()    // ActiveModel::Validations mixin + Errors/Error + Naming/Name + Validator/EachValidator (require "active_model"), backed by go-ruby-activemodel; Attr/Dispatcher/Condition seams wired to rbgo (ivar get/set + method dispatch + Ruby if/unless procs)
	vm.registerAASM()           // AASM mixin: `include AASM` + `aasm do state/event/transitions … end` DSL, per-event fire/fire!/may_fire? + per-state predicates, obj.aasm reader (require "aasm"), backed by go-ruby-aasm; guards/callbacks run INLINE under the GVL, state/persist seams wired to the object's column ivar + save; needs StandardError for AASM::Error
	vm.registerActiveStorage()  // ActiveStorage::Blob/Service/Attachment + Attached::One/Many (require "active_storage"), backed by go-ruby-activestorage; ModelStore/Service/Signer/Random/Clock seams wired to a deterministic in-process config (MemStore + DiskService temp dir); needs StandardError for ActiveStorage::Error
	vm.registerActionCable()    // ActionCable::Server/Connection::Base/Channel::Base + ActionCable.server (require "action_cable"), backed by go-ruby-actioncable; the transport/channel-action/factory seams eval Ruby bodies + the async adapter default keep the byte-exact actioncable-v1-json wire protocol in the library; needs StandardError (ActionCable::Error)
	vm.registerActionPack()     // ActionController::Base + ActionController::Parameters + ActionDispatch::Routing::RouteSet/Request/Response (require "action_controller" / "action_dispatch"), backed by go-ruby-actionpack; the action body (RunAction), the before/after/around filters, rescue_from and the view render (Renderer -> an ActionView-style context) are the rbgo seams run inline under the GVL; the route compiler/recognizer/generator, filter/rescue pipeline and strong-params semantics are the library; needs StandardError + Rack, so registered after registerRack/registerActionCable
	vm.registerDevise()         // Devise::Config/Resource/Encryptor/TokenGenerator + Strategies::DatabaseAuthenticatable (require "devise"), backed by go-ruby-devise over go-ruby-bcrypt + go-ruby-warden; the model #[]/#[]=/#save + finder callable are the rbgo seams, and the strategy plugs into the bound Warden registry; needs StandardError (Devise::Error). Registered after registerWarden + registerBCrypt (dependency order)
	vm.registerActiveRecord()   // ActiveRecord::Model/Relation/Record + Base.establish_connection (require "active_record"), backed by go-ruby-activerecord; adapter seam wired to go-ruby-sqlite3 so queries run; needs StandardError for ActiveRecordError
	vm.registerKaminari()       // Kaminari::PaginatableArray/PaginatableRelation + Array#page + Model/relation #page (require "kaminari"), backed by go-ruby-kaminari; the Relation seam wires #count/#offset/#limit to an ActiveRecord relation, so registered after registerActiveRecord
	vm.registerPaperTrail()     // PaperTrail::Version/RecordTrail/Request + has_paper_trail macro (require "paper_trail"), backed by go-ruby-paper-trail; snapshot=model @ivars, store=shared MemoryStore, clock=time.Now, whodunnit/enabled=PaperTrail.request seams
	vm.registerRansack()        // Ransack module + Ransack::Search/Sort (require "ransack"), backed by go-ruby-ransack; Model.ransack(q).result / Ransack.search(subject, q); the record-source Backend seam fetches the subject's rows via #all and runs the engine's in-memory Match/Apply evaluator; must run after registerActiveRecord so ActiveRecord::Base gains .ransack/.search
	vm.registerRQRCode()        // RQRCode::QRCode (require "rqrcode"), backed by go-ruby-rqrcode; needs StandardError for RQRCode::QRCode*Error
	vm.registerPagy()           // Pagy (require "pagy"), backed by go-ruby-pagy; needs StandardError for Pagy::OverflowError / Pagy::VariableError
	vm.registerDotenv()         // Dotenv module (require "dotenv"), backed by go-ruby-dotenv; wires ENV read/write + shell seams
	vm.registerHCL2()           // HCL2 module (require "hcl2"), backed by go-ruby-hcl2; needs StandardError for HCL2::Error
	vm.registerI18n()           // I18n module (require "i18n"), backed by go-ruby-i18n; needs ArgumentError for the I18n::ArgumentError tree
	vm.registerZeitwerk()       // Zeitwerk::Loader/Inflector (require "zeitwerk"), backed by go-ruby-zeitwerk; wires DefineAutoload->Module#autoload, Load->require; needs StandardError/NameError for the Zeitwerk error tree
	vm.registerRSS()            // RSS::Parser.parse -> RSS::Rss / RSS::RDF / RSS::Atom::Feed (require "rss"), backed by go-ruby-rss; needs StandardError for RSS::Error
	vm.registerRDoc()           // RDoc::Markup + ToHtml/ToMarkdown/ToRdoc formatters (require "rdoc"), backed by go-ruby-rdoc; needs StandardError for RDoc::Error
	vm.registerThor()           // Thor CLI framework: option parsing + dispatch + help (require "thor"), backed by go-ruby-thor; needs StandardError for Thor::Error
	vm.registerRacc()           // Racc::Parser LALR(1) runtime (require "racc/parser"), backed by go-ruby-racc; needs StandardError for Racc::ParseError
	vm.registerKramdown()       // Kramdown::Document (require "kramdown"), backed by go-kramdown
	vm.registerImages()         // Images::Image/Canvas/Color (require "images"), backed by go-ruby-images (MiniMagick/ruby-vips processing + chunky_png canvas + scikit-image ops); needs StandardError for Images::Error
	vm.registerOpentype()       // Opentype module + Opentype::Font/Face (require "opentype"), backed by go-ruby-opentype over the go-opentype text stack (font parsing, sized faces, complex-script shaping, Unicode Bidi); every adapter method installed via its Methods()/Call() surface; needs StandardError for Opentype::Error
	vm.registerWidgets()        // Widgets module (require "widgets"), backed by go-ruby-widgets/widgets over the go-widgets pixel-blitting toolkit: constructors + flex/fixed/grid/border/card composition + layout/render (RGBA buffer)/dispatch, addressed by integer handles; needs StandardError for Widgets::Error
	vm.registerTui()            // Tui module + Tui::Widget (require "tui"), backed by go-ruby-widgets/tui over the go-widgets terminal-cell toolkit: constructors/composition + render (ANSI)/render_cells/decode_cells/dispatch; needs StandardError for Tui::Error
	vm.registerMvvm()           // Mvvm module + Mvvm::Observable/Command/ObservableList (require "mvvm"), backed by go-ruby-widgets/mvvm over the go-widgets data-binding layer; notifications pull through drain_events; needs StandardError for Mvvm::Error
	vm.registerShrine()         // Shrine file-attachment (require "shrine"), backed by go-ruby-shrine; Storage(Memory/FileSystem)/Uploader/UploadedFile/Attacher; needs StandardError for Shrine::Error + StringIO for #open
	vm.registerLiquid()         // Liquid::Template.parse(...).render (require "liquid"), backed by go-liquid; needs StandardError for Liquid::Error
	vm.registerSass()           // Sass.compile / compile_string / compile_file + SassC::Engine (require "sass"), backed by go-ruby-sass over the pure-Go go-scss engine; needs StandardError for Sass::CompileError / SassC::SyntaxError
	vm.registerRouge()          // Rouge.highlight / Rouge::Lexer.find (require "rouge"), backed by go-rouge; needs StandardError for Rouge::Error
	vm.registerSlim()           // Slim::Template.new{src}.render (require "slim"), compile-to-source via go-ruby-slim; needs StandardError for Slim::Error
	vm.registerHaml()           // Haml::Template.new(src).render (require "haml"), compile-to-source via go-ruby-haml; needs StandardError for Haml::Error
	vm.registerDryTypes()       // Dry::Types type system (require "dry/types"), backed by go-ruby-dry-types; needs StandardError for Dry::Types::CoercionError
	vm.registerDryStruct()      // Dry::Struct base class (require "dry/struct"), backed by go-ruby-dry-struct; pins go-ruby-dry-types
	vm.registerDryValidation()  // Dry::Schema / Dry::Validation::Contract (require "dry/validation"), backed by go-ruby-dry-validation; pins go-ruby-dry-types
	vm.registerOAuth2()         // OAuth2::Client / AccessToken / Response (require "oauth2"), backed by go-ruby-oauth2; HTTP round-trip is a net-http host seam
	vm.registerOIDC()           // OpenIDConnect::Client / Verifier / ProviderMetadata (require "openid_connect" / "oidc"), backed by go-ruby-oidc; reuses OAuth2+JWT; HTTP seam is a Ruby callable Doer
	vm.registerOpenStack()      // OpenStack.connect -> OpenStack::Connection + the six service accessors (compute/network/block_storage/object_storage/image/identity) with per-resource CRUD (require "openstack"), backed by go-ruby-openstack over gophercloud; resources cross as Ruby Hashes, the HTTP transport is the vm.osTransport injection seam; needs StandardError for the OpenStack::Error tree
}

// registerGetoptLongGem is GetoptLong (bootstrap, after FileUtils).
func (vm *VM) registerGetoptLongGem() {
	vm.registerGetoptLong() // GetoptLong loadable shell; needs StandardError
}

// registerToolingGems is process, terminal and document tooling (bootstrap,
// after Signal).
func (vm *VM) registerToolingGems() {
	vm.registerOpen3()  // Open3 loadable shell (popen/capture raise; spawning pending)
	vm.registerRipper() // ripper loadable shell (Ripper.sexp etc. raise); needs StandardError
	vm.registerSyslog() // Syslog loadable shell (feature probe)
	vm.registerCGI()    // CGI.escape/unescape (real over net/url) + HTML helpers
	vm.registerERB()    // ERB class skeleton + ERB::Util (backed by go-ruby-erb); prelude adds the Ruby API
	vm.registerIRB()    // IRB module + REPL (require "irb"), backed by go-ruby-irb; evaluates each statement through rbgo; needs Binding + IO
}

// registerOptparseURIGems is OptionParser and URI (bootstrap, after
// StringScanner).
func (vm *VM) registerOptparseURIGems() {
	vm.registerOptionParser() // OptionParser (require "optparse"), backed by go-ruby-optparse; prelude adds the ParseError tree
	vm.registerURI()          // URI module (require "uri"), backed by go-ruby-uri; needs StandardError (URI::Error) + Regexp (make_regexp)
}

// registerREXMLGem is REXML (bootstrap, after CSV).
func (vm *VM) registerREXMLGem() {
	vm.registerREXML() // REXML module (require "rexml/document"), backed by go-ruby-rexml; needs StandardError (REXML::ParseException)
}

// registerArrowShellwordsGems is Arrow and Shellwords (bootstrap, after Matrix).
func (vm *VM) registerArrowShellwordsGems() {
	vm.registerArrow()      // Arrow module (require "arrow"), backed by go-ruby-arrow; needs StandardError (Arrow::Error)
	vm.registerShellwords() // Shellwords (require "shellwords"), backed by go-ruby-shellwords; installs Shellwords + String/Array core extensions lazily on require
}

// registerZlibGem is Zlib (bootstrap, after Observable).
func (vm *VM) registerZlibGem() {
	vm.registerZlib() // needs the exception hierarchy (Zlib::Error < StandardError)
}

// registerConcurrentGem is the concurrent-ruby shell (bootstrap, after Etc).
func (vm *VM) registerConcurrentGem() {
	vm.registerConcurrent() // concurrent-ruby shell (collections alias core; ThreadLocalVar)
}

// registerRailsSupportGems is ActiveSupport and ActionView (NewWithStderr,
// right after the prelude and Enumerator).
func (vm *VM) registerRailsSupportGems() {
	vm.registerActiveSupport() // ActiveSupport::Inflector + core extensions (require "active_support" / "active_support/all"), backed by go-ruby-activesupport; after the prelude so the Enumerable module (which its core-ext extends) exists
	vm.registerActionView()    // ActionView::Base view context (tag/url/form/text/number helpers + render) + FormBuilder + PartialIteration + ActiveSupport::SafeBuffer / String#html_safe (require "action_view"), backed by go-ruby-actionview; the URLFor (routes) and RenderTemplate seams wire to Ruby callables, the inline-render default evals ERB through the already-bound go-ruby-erb compiler; after registerActiveSupport (SafeBuffer nests under ActiveSupport) and after the bootstrap ERB/Erubi registration (escaping/compiler surface); #render stays a dispatchable method for a later actionpack/actionmailer binding
}

// registerIPAddrGem is IPAddr (NewWithStderr, after File::Stat).
func (vm *VM) registerIPAddrGem() {
	vm.registerIPAddr() // IPAddr (require "ipaddr"), backed by go-ruby-ipaddr; after the prelude so IPAddr can mix in Comparable
}

// registerPStoreGem is PStore (NewWithStderr, after Logger).
func (vm *VM) registerPStoreGem() {
	vm.registerPStore() // PStore (require "pstore"), backed by go-ruby-pstore; after the prelude so PStore::Error < StandardError
}

// includeMySQLEnumerableGem mixes Enumerable into Mysql2::Result
// (NewWithStderr, after TracePoint).
func (vm *VM) includeMySQLEnumerableGem() {
	vm.includeMySQLEnumerable() // Mysql2::Result mixes in Enumerable; after the prelude so the module exists
}
