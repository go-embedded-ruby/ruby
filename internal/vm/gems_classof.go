// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	rake "github.com/go-ruby-rake/rake"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// classOfGem maps the value types of the gem bindings to their Ruby class. These
// cases used to sit in classOf's own switch; they moved here, unchanged, when the
// rbgo_core build stopped compiling the files that define the types (gems.go
// says why). classOf consults this after its main switch misses, like
// classOfExtBinding: the gem types are disjoint from the core ones, so the order
// changes no answer, and a core value no longer walks past ~330 gem cases on its
// way to its own. gems_core.go's version reports false for everything.
func (vm *VM) classOfGem(v object.Value) (*RClass, bool) {
	switch x := v.(type) {
	case *Image:
		return vm.cImage, true
	case *OptionParser:
		return vm.cOptionParser, true
	case *URI:
		// A URI wrapper carries its own Ruby class (URI::Generic or a scheme
		// subclass such as URI::HTTP), so it reports that class for `class` /
		// `is_a?` while every instance method lives on URI::Generic.
		return x.cls, true
	case *ALDirectoryObj:
		return x.cls, true
	case *ALConnectionObj:
		return x.cls, true
	case *ALModelObj:
		return x.cls, true
	case *ALRecordObj:
		return x.cls, true
	case *LDAPConn:
		// The Net::LDAP binding builds for every target (its go-ldap transport is
		// pure-Go net code that compiles for wasm), so its value types carry their
		// class here in the shared switch rather than in classOfExtBinding.
		return x.cls, true
	case *LDAPEntry:
		return x.cls, true
	case *LDAPFilter:
		return x.cls, true
	case *LDAPResult:
		return x.cls, true
	case *RackRequest:
		// A Rack::Request wrapper reports the class stamped on it at construction.
		return x.cls, true
	case *RackResponse:
		return x.cls, true
	case *friendlyIDScope:
		// The scope object Model.friendly returns; its #find lives on
		// FriendlyId::FinderMethods (stamped on the scope at construction).
		return x.cls, true
	case *SinatraCtx:
		// The self a Sinatra route/filter block runs against.
		return x.cls, true
	case *SinatraSettings:
		return x.cls, true
	case *RodaReq:
		// The self a Roda route/matcher block runs against (Roda::RodaRequest).
		return x.cls, true
	case *RodaResp:
		return x.cls, true
	case *AsyncTask:
		return x.cls, true
	case *AsyncBarrier:
		return x.cls, true
	case *AsyncSemaphore:
		return x.cls, true
	case *AsyncCondition:
		return x.cls, true
	case *AsyncNotification:
		return x.cls, true
	case *AsyncQueue:
		return x.cls, true
	case *AsyncLimitedQueue:
		return x.cls, true
	case *AsyncWaiter:
		return x.cls, true
	case *REXMLDocument:
		return vm.cREXMLDocument, true
	case *REXMLElement:
		return vm.cREXMLElement, true
	case *REXMLElements:
		return vm.cREXMLElements, true
	case *REXMLAttributes:
		return vm.cREXMLAttributes, true
	case *REXMLText:
		return vm.cREXMLText, true
	case *REXMLComment:
		return vm.cREXMLComment, true
	case *REXMLCData:
		return vm.cREXMLCData, true
	case *REXMLInstruction:
		return vm.cREXMLInstruction, true
	case *REXMLDocType:
		return vm.cREXMLDocType, true
	case *rolifyRole:
		// A rolify role wrapper (user.roles / resource.applied_roles) reports the
		// Rolify::Role class stamped on it, whose name/resource_type/resource_id/id
		// methods read the wrapped role.
		return x.cls, true
	case *GetoptLong:
		return vm.cGetoptLong, true
	case *PStore:
		return vm.cPStore, true
	case *Jbuilder:
		// A Jbuilder builder reports Jbuilder so its method_missing DSL and bang
		// methods (defined on that class) dispatch.
		return vm.consts["Jbuilder"].(*RClass), true
	case *FactoryBotProxy:
		// The self a factory/trait/transient/nested-factory block runs against; its
		// method_missing + DSL methods live on FactoryBot::DefinitionProxy.
		return vm.consts["FactoryBot::DefinitionProxy"].(*RClass), true
	case *FactoryBotTopProxy:
		// The self a FactoryBot.define block runs against (factory/sequence).
		return vm.consts["FactoryBot::Syntax::Default::DSL"].(*RClass), true
	case *FactoryBotEvaluator:
		// The self a dynamic attribute block / callback reads siblings through.
		return vm.consts["FactoryBot::Evaluator"].(*RClass), true
	case *FacterFact:
		// The handle Facter[]/Facter.fact returns; #value/#name live on it.
		return vm.consts["Facter::Util::Fact"].(*RClass), true
	case *FacterResolution:
		// The self a Facter.add block configures (setcode/confine/has_weight).
		return vm.consts["Facter::Util::Resolution"].(*RClass), true
	case *HieraObj:
		// A Hiera instance (require "hiera"); #lookup lives on the Hiera class.
		return vm.consts["Hiera"].(*RClass), true
	case *PuppetCatalog:
		// A compiled catalog (require "puppet"); Puppet::Resource::Catalog methods.
		return vm.consts["Puppet::Resource::Catalog"].(*RClass), true
	case *PuppetResource:
		// One catalog resource; Puppet::Resource methods.
		return vm.consts["Puppet::Resource"].(*RClass), true
	case *PraType:
		// A registered resource type (require "puppet/resource_api");
		// #validate/#attributes/#namevars/#apply live on it.
		return vm.consts["Puppet::ResourceApi::TypeDefinition"].(*RClass), true
	case *SemVerObj:
		// A SemanticPuppet::Version (require "semantic_puppet").
		return vm.consts["SemanticPuppet::Version"].(*RClass), true
	case *SemVerRange:
		// A SemanticPuppet::VersionRange (require "semantic_puppet").
		return vm.consts["SemanticPuppet::VersionRange"].(*RClass), true
	case *AugeasObj:
		// An Augeas config-tree handle (require "augeas").
		return vm.consts["Augeas"].(*RClass), true
	case *HoconConfig:
		// A Hocon::Config (require "hocon"); typed path accessors live on it.
		return vm.consts["Hocon::Config"].(*RClass), true
	case *DryType:
		return vm.consts["Dry::Types::Type"].(*RClass), true
	case *DryResult:
		return vm.consts["Dry::Types::Result"].(*RClass), true
	case *DryStruct:
		return x.cls, true
	case *DrySchema:
		return vm.consts["Dry::Schema::Params"].(*RClass), true
	case *DrySchemaBuilder:
		return vm.consts["Dry::Schema::DSL"].(*RClass), true
	case *DryKey:
		return vm.consts["Dry::Schema::Key"].(*RClass), true
	case *DryContract:
		return x.cls, true
	case *DryValidationResult:
		return vm.consts["Dry::Validation::Result"].(*RClass), true
	case *DryRuleCtx:
		return vm.consts["Dry::Validation::Rule"].(*RClass), true
	case *DryRuleKey:
		return vm.consts["Dry::Validation::RuleKey"].(*RClass), true
	case *OAuth2Client:
		return vm.consts["OAuth2::Client"].(*RClass), true
	case *OAuth2Strategy:
		return vm.consts[x.className].(*RClass), true
	case *OAuth2AccessToken:
		return vm.consts["OAuth2::AccessToken"].(*RClass), true
	case *OAuth2Response:
		return vm.consts["OAuth2::Response"].(*RClass), true
	case *OAuth2Request:
		return vm.consts["OAuth2::Request"].(*RClass), true
	case *OIDCProviderMetadata:
		return vm.consts["OpenIDConnect::ProviderMetadata"].(*RClass), true
	case *OIDCKeySet:
		return vm.consts["OpenIDConnect::KeySet"].(*RClass), true
	case *OIDCVerifier:
		return vm.consts["OpenIDConnect::Verifier"].(*RClass), true
	case *OIDCClaims:
		return vm.consts["OpenIDConnect::IDTokenClaims"].(*RClass), true
	case *OIDCClient:
		return vm.consts["OpenIDConnect::Client"].(*RClass), true
	case *OIDCTokens:
		return vm.consts["OpenIDConnect::Tokens"].(*RClass), true
	case *OIDCUserInfo:
		return vm.consts["OpenIDConnect::UserInfo"].(*RClass), true
	case *KramdownDoc:
		// A Kramdown::Document reports Kramdown::Document so its #to_html renders
		// the held source through the library.
		return vm.consts["Kramdown::Document"].(*RClass), true
	case *I18nBackend:
		// The handle I18n.backend returns reports I18n::Backend::Simple so its
		// store_translations / available_locales dispatch.
		return vm.consts["I18n::Backend::Simple"].(*RClass), true
	case *ZeitwerkLoader:
		// A Zeitwerk::Loader.new / .for_gem handle reports Zeitwerk::Loader so its
		// push_dir / setup / eager_load / … instance methods dispatch.
		return vm.consts["Zeitwerk::Loader"].(*RClass), true
	case *ZeitwerkInflector:
		// A Zeitwerk::Inflector handle reports Zeitwerk::Inflector so its
		// camelize / inflect dispatch.
		return vm.consts["Zeitwerk::Inflector"].(*RClass), true
	case *RSSRss:
		return vm.consts["RSS::Rss"].(*RClass), true
	case *RSSChannel:
		return vm.consts["RSS::Rss::Channel"].(*RClass), true
	case *RSSItem:
		return vm.consts["RSS::Rss::Channel::Item"].(*RClass), true
	case *RSSImage:
		return vm.consts["RSS::Rss::Channel::Image"].(*RClass), true
	case *RSSGuid:
		return vm.consts["RSS::Rss::Channel::Item::Guid"].(*RClass), true
	case *RSSRDF:
		return vm.consts["RSS::RDF"].(*RClass), true
	case *RSSRDFChannel:
		return vm.consts["RSS::RDF::Channel"].(*RClass), true
	case *RSSRDFItem:
		return vm.consts["RSS::RDF::Item"].(*RClass), true
	case *RSSRDFImage:
		return vm.consts["RSS::RDF::Image"].(*RClass), true
	case *RSSRDFTextinput:
		return vm.consts["RSS::RDF::Textinput"].(*RClass), true
	case *RSSAtomFeed:
		return vm.consts["RSS::Atom::Feed"].(*RClass), true
	case *RSSAtomEntry:
		return vm.consts["RSS::Atom::Feed::Entry"].(*RClass), true
	case *RSSAtomLink:
		return vm.consts["RSS::Atom::Link"].(*RClass), true
	case *RSSAtomPerson:
		return vm.consts["RSS::Atom::Person"].(*RClass), true
	case *RSSAtomCategory:
		return vm.consts["RSS::Atom::Category"].(*RClass), true
	case *RQRCode:
		// An RQRCode::QRCode reports RQRCode::QRCode so its renderer methods
		// (as_svg / as_ansi / as_html / to_s / checked? / to_a) dispatch.
		return vm.consts["RQRCode::QRCode"].(*RClass), true
	case *ImagesImage:
		// An Images::Image reports Images::Image so its processing + scikit-image
		// methods (resize / crop / rotate / convert / blur / edges / at / to_blob)
		// dispatch.
		return vm.consts["Images::Image"].(*RClass), true
	case *ImagesCanvas:
		// An Images::Canvas reports Images::Canvas so its chunky_png-style pixel,
		// drawing and PNG-output methods dispatch.
		return vm.consts["Images::Canvas"].(*RClass), true
	case *OpentypeFont:
		// A parsed font reports Opentype::Font so its num_glyphs / glyph_index /
		// axes / named_instances / face methods dispatch.
		return vm.consts["Opentype::Font"].(*RClass), true
	case *OpentypeFace:
		// A sized face reports Opentype::Face so its measure / advance / kern /
		// metrics / glyph_info / set_hinting / set_variation methods dispatch.
		return vm.consts["Opentype::Face"].(*RClass), true
	case *TuiWidget:
		// A Tui::Widget handle reports Tui::Widget so its mutators, composition
		// verbs and callback-wiring instance methods dispatch.
		return vm.consts["Tui::Widget"].(*RClass), true
	case *MvvmObservable:
		// An Mvvm::Observable handle reports Mvvm::Observable so its get / set /
		// subscribe / unsubscribe instance methods dispatch.
		return vm.consts["Mvvm::Observable"].(*RClass), true
	case *MvvmCommand:
		// An Mvvm::Command handle reports Mvvm::Command so its can_execute /
		// execute / set_can_execute / raise_can_execute_changed methods dispatch.
		return vm.consts["Mvvm::Command"].(*RClass), true
	case *MvvmList:
		// An Mvvm::ObservableList handle reports Mvvm::ObservableList so its
		// add / insert / remove_at / set / move / clear / get / size / slice /
		// observe / unobserve instance methods dispatch.
		return vm.consts["Mvvm::ObservableList"].(*RClass), true
	case *XmlMarkup:
		// A Builder::XmlMarkup emitter reports Builder::XmlMarkup so its
		// method_missing element DSL and special methods dispatch.
		return vm.consts["Builder::XmlMarkup"].(*RClass), true
	case *SimpleCovResult:
		return x.cls, true
	case *SimpleCovSourceFile:
		return x.cls, true
	case *SAMLSettings:
		return x.cls, true
	case *SAMLAuthrequest:
		return x.cls, true
	case *SAMLResponse:
		return x.cls, true
	case *SAMLMetadata:
		return x.cls, true
	case *SAMLIdpMetadataParser:
		return x.cls, true
	case *SAMLLogoutrequest:
		return x.cls, true
	case *SAMLSloLogoutresponse:
		return x.cls, true
	case *WebAuthnRelyingParty:
		return x.cls, true
	case *WebAuthnPublicKey:
		return x.cls, true
	case *WebAuthnCredential:
		return x.cls, true
	case *WebAuthnCreateOptions:
		return x.cls, true
	case *WebAuthnGetOptions:
		return x.cls, true
	case *ACMEClient:
		return x.cls, true
	case *ACMEOrder:
		return x.cls, true
	case *ACMEAuthorization:
		return x.cls, true
	case *ACMEChallenge:
		return x.cls, true
	case *ACMECSR:
		return x.cls, true
	case *VaultClient:
		return x.cls, true
	case *VaultLogical:
		return x.cls, true
	case *VaultKVv1:
		return x.cls, true
	case *VaultKVv2:
		return x.cls, true
	case *VaultTransit:
		return x.cls, true
	case *VaultSys:
		return x.cls, true
	case *VaultAuth:
		return x.cls, true
	case *VaultTokenAuth:
		return x.cls, true
	case *VaultAppRole:
		return x.cls, true
	case *VaultUserpass:
		return x.cls, true
	case *VaultSecret:
		return x.cls, true
	case *HTTPartyResponse:
		return vm.consts["HTTParty::Response"].(*RClass), true
	case *RConnectionPool:
		return x.cls, true
	case *RConnectionPoolWrapper:
		return x.cls, true
	case *ConcurrentAtomicRef:
		return x.cls, true
	case *ConcurrentAtomicFixnum:
		return x.cls, true
	case *ConcurrentAtomicBool:
		return x.cls, true
	case *ConcurrentMap:
		return x.cls, true
	case *ConcurrentFuture:
		return x.cls, true
	case *ConcurrentPromise:
		return x.cls, true
	case *ConcurrentPool:
		return x.cls, true
	case *ConcurrentLatch:
		return x.cls, true
	case *ConcurrentSemaphore:
		return x.cls, true
	case *ConcurrentBarrier:
		return x.cls, true
	case *ASInflections:
		return vm.consts["ActiveSupport::Inflector::Inflections"].(*RClass), true
	case *ErubiEngine:
		return x.cls, true
	case *SafeBufferVal:
		return x.cls, true
	case *ActionViewBase:
		return x.cls, true
	case *AVPartialIter:
		return x.cls, true
	case *FormBuilderVal:
		return x.cls, true
	case *HTTPrbClient:
		return vm.consts["HTTP::Client"].(*RClass), true
	case *HTTPrbResponse:
		return vm.consts["HTTP::Response"].(*RClass), true
	case *HTTPrbStatus:
		return vm.consts["HTTP::Response::Status"].(*RClass), true
	case *ExconConnection:
		return vm.consts["Excon::Connection"].(*RClass), true
	case *ExconResponse:
		return vm.consts["Excon::Response"].(*RClass), true
	case *TyphoeusRequest:
		return vm.consts["Typhoeus::Request"].(*RClass), true
	case *TyphoeusResponse:
		return vm.consts["Typhoeus::Response"].(*RClass), true
	case *TyphoeusHydra:
		return vm.consts["Typhoeus::Hydra"].(*RClass), true
	case *IMAPObj:
		return x.cls, true
	case *sftpSession:
		return x.cls, true
	case *sftpDir:
		return x.cls, true
	case *sftpFileFactory:
		return x.cls, true
	case *sftpFile:
		return x.cls, true
	case *sftpAttrs:
		return x.cls, true
	case *sftpName:
		return x.cls, true
	case *RedisObj:
		return x.cls, true
	case *RedisBatch:
		return x.cls, true
	case *PTVersion:
		return vm.consts["PaperTrail::Version"].(*RClass), true
	case *PTProxy:
		return vm.consts["PaperTrail::RecordTrail"].(*RClass), true
	case *PTRequest:
		return vm.consts["PaperTrail::Request"].(*RClass), true
	case *POP3Obj:
		return x.cls, true
	case *POPMailObj:
		return x.cls, true
	case *ftpObj:
		return x.cls, true
	case *ftpMLSxEntry:
		return x.cls, true
	case *PGConnObj:
		return x.cls, true
	case *PGResultObj:
		return x.cls, true
	case *SMTPObj:
		return x.cls, true
	case *SMTPResponseObj:
		return x.cls, true
	case *SMTPStreamObj:
		return x.cls, true
	case *NokogiriDocument:
		return vm.consts["Nokogiri::XML::Document"].(*RClass), true
	case *NokogiriNode:
		return vm.consts["Nokogiri::XML::Node"].(*RClass), true
	case *NokogiriNodeSet:
		return vm.consts["Nokogiri::XML::NodeSet"].(*RClass), true
	case *NokogiriXSLTStylesheet:
		return vm.consts["Nokogiri::XSLT::Stylesheet"].(*RClass), true
	case *RuboCopRunner:
		return vm.consts["RuboCop::Runner"].(*RClass), true
	case *RuboCopConfig:
		return vm.consts["RuboCop::Config"].(*RClass), true
	case *RuboCopOffense:
		return vm.consts["RuboCop::Cop::Offense"].(*RClass), true
	case *RuboCopLocation:
		return vm.consts["RuboCop::Cop::Offense::Location"].(*RClass), true
	case *GrapeRouter:
		return vm.consts["Grape::Router"].(*RClass), true
	case *GrapeRoute:
		return vm.consts["Grape::Router::Route"].(*RClass), true
	case *GrapeMatch:
		return vm.consts["Grape::Router::Match"].(*RClass), true
	case *GrapeValidator:
		return vm.consts["Grape::Validator"].(*RClass), true
	case *GrapeParamsBuilder:
		return vm.consts["Grape::Validations::ParamsScope::DSL"].(*RClass), true
	case *GrapeFormatter:
		return vm.consts["Grape::Formatter"].(*RClass), true
	case *ActiveRecordModel:
		return vm.consts["ActiveRecord::Model"].(*RClass), true
	case *ActiveRecordModelBuilder:
		return vm.consts["ActiveRecord::Model::DSL"].(*RClass), true
	case *ActiveRecordRelation:
		return vm.consts["ActiveRecord::Relation"].(*RClass), true
	case *ActiveRecordRecord:
		return vm.consts["ActiveRecord::Record"].(*RClass), true
	case *ActiveRecordErrors:
		return vm.consts["ActiveRecord::Errors"].(*RClass), true
	case *KaminariArray:
		return vm.consts["Kaminari::PaginatableArray"].(*RClass), true
	case *KaminariRelation:
		return vm.consts["Kaminari::PaginatableRelation"].(*RClass), true
	case *RansackSearch:
		return vm.consts["Ransack::Search"].(*RClass), true
	case *RansackSort:
		return vm.consts["Ransack::Sort"].(*RClass), true
	case *ActiveRecordSchemaDSL:
		return vm.consts["ActiveRecord::Schema::Definition"].(*RClass), true
	case *ActiveRecordTableDSL:
		return vm.consts["ActiveRecord::Schema::TableDefinition"].(*RClass), true
	case *ASModelName:
		return vm.consts["ActiveModel::Name"].(*RClass), true
	case *ASModelErrors:
		return vm.consts["ActiveModel::Errors"].(*RClass), true
	case *ASModelError:
		return vm.consts["ActiveModel::Error"].(*RClass), true
	case *ActiveJobConfigured:
		return vm.consts["ActiveJob::ConfiguredJob"].(*RClass), true
	case *ASBlob:
		return vm.consts["ActiveStorage::Blob"].(*RClass), true
	case *ASAttachment:
		return vm.consts["ActiveStorage::Attachment"].(*RClass), true
	case *ASService:
		return vm.consts["ActiveStorage::Service"].(*RClass), true
	case *ASOne:
		return vm.consts["ActiveStorage::Attached::One"].(*RClass), true
	case *ASMany:
		return vm.consts["ActiveStorage::Attached::Many"].(*RClass), true
	case *RailtieVal:
		return x.cls, true
	case *EngineVal:
		return x.cls, true
	case *RailsAppVal:
		return x.cls, true
	case *RailsConfigVal:
		return x.cls, true
	case *RailsPathsVal:
		return x.cls, true
	case *RailsPathVal:
		return x.cls, true
	case *RailsRouteSetVal:
		return x.cls, true
	case *RailsInitializerVal:
		return x.cls, true
	case *StringInquirerVal:
		return x.cls, true
	case *RailsEnvVal:
		return x.cls, true
	case *DeviseConfig:
		return x.cls, true
	case *DeviseResource:
		return x.cls, true
	case *DeviseTokenGenerator:
		return x.cls, true
	case *HanamiRouter:
		return x.cls, true
	case *HanamiReq:
		return x.cls, true
	case *HanamiResp:
		return x.cls, true
	case *HanamiFlash:
		return x.cls, true
	case *RSpecMatcher:
		return vm.consts["RSpec::Matchers::BuiltIn::BaseMatcher"].(*RClass), true
	case *RSpecExpectation:
		return vm.consts["RSpec::Expectations::ExpectationTarget"].(*RClass), true
	case *IPAddr:
		return vm.cIPAddr, true
	case *Timezone:
		return vm.consts["TZInfo::Timezone"].(*RClass), true
	case *TimezonePeriod:
		return vm.consts["TZInfo::TimezonePeriod"].(*RClass), true
	case *TimezoneOffset:
		return vm.consts["TZInfo::TimezoneOffset"].(*RClass), true
	case *Country:
		return vm.consts["TZInfo::Country"].(*RClass), true
	case *Pagy:
		return vm.consts["Pagy"].(*RClass), true
	case *Money:
		return vm.consts["Money"].(*RClass), true
	case *Currency:
		return vm.consts["Money::Currency"].(*RClass), true
	case *AddressableURI:
		return vm.consts["Addressable::URI"].(*RClass), true
	case *AddressableTemplate:
		return vm.consts["Addressable::Template"].(*RClass), true
	case *PublicSuffixDomain:
		return vm.consts["PublicSuffix::Domain"].(*RClass), true
	case *MIMEType:
		return vm.consts["MIME::Type"].(*RClass), true
	case *MailMessage:
		return vm.consts["Mail::Message"].(*RClass), true
	case *MailBody:
		return vm.consts["Mail::Body"].(*RClass), true
	case *MailField:
		return vm.consts["Mail::Field"].(*RClass), true
	case *ActionMailerDelivery:
		return x.cls, true
	case *ActionMailerAttachments:
		return x.cls, true
	case *RelineHistory:
		return x.cls, true
	case *BCryptPassword:
		return x.cls, true
	case *naclObj:
		return x.cls, true
	case *AgeX25519Identity:
		return x.cls, true
	case *AgeX25519Recipient:
		return x.cls, true
	case *AgeScryptIdentity:
		return x.cls, true
	case *AgeScryptRecipient:
		return x.cls, true
	case *PrawnDocument:
		return x.cls, true
	case *ProtobufPool:
		return x.cls, true
	case *ProtobufBuilder:
		return x.cls, true
	case *ProtobufMessageBuilder:
		return x.cls, true
	case *ProtobufOneofBuilder:
		return x.cls, true
	case *ProtobufEnumBuilder:
		return x.cls, true
	case *ProtobufDescriptor:
		return x.cls, true
	case *ProtobufEnumDescriptor:
		return x.cls, true
	case *ProtobufFieldDescriptor:
		return x.cls, true
	case *ProtobufMsgClass:
		return x.cls, true
	case *ProtobufMessage:
		return x.cls, true
	case *ProtobufRepeatedField:
		return x.cls, true
	case *ProtobufMap:
		return x.cls, true
	case *GraphQLType:
		return x.cls, true
	case *GraphQLSchema:
		return x.cls, true
	case *GraphQLObjectDSL:
		return x.cls, true
	case *OTelTracerProvider:
		return x.cls, true
	case *OTelTracer:
		return vm.consts["OpenTelemetry::Trace::Tracer"].(*RClass), true
	case *OTelSpan:
		return vm.consts["OpenTelemetry::Trace::Span"].(*RClass), true
	case *OTelSpanContext:
		return vm.consts["OpenTelemetry::Trace::SpanContext"].(*RClass), true
	case *OTelStatus:
		return vm.consts["OpenTelemetry::Trace::Status"].(*RClass), true
	case *OTelExporter:
		return vm.consts["OpenTelemetry::SDK::Trace::Export::InMemorySpanExporter"].(*RClass), true
	case *OTelProcessor:
		return x.cls, true
	case *OTelFinishedSpan:
		return vm.consts["OpenTelemetry::SDK::Trace::SpanData"].(*RClass), true
	case *RDocMarkup:
		return vm.consts["RDoc::Markup"].(*RClass), true
	case *RDocFormatter:
		return vm.consts[x.clsName].(*RClass), true
	case *ThorOption:
		return vm.consts["Thor::Option"].(*RClass), true
	case *ThorOptions:
		return vm.consts["Thor::Options"].(*RClass), true
	case *ThorCommand:
		return vm.consts["Thor::Command"].(*RClass), true
	case *ThorBase:
		return vm.consts["Thor::Base"].(*RClass), true
	case *RakeTaskVal:
		if _, isFile := x.t.(*rake.FileTask); isFile {
			return vm.consts["Rake::FileTask"].(*RClass), true
		}
		return vm.consts["Rake::Task"].(*RClass), true
	case *RakeApplicationVal:
		return vm.consts["Rake::Application"].(*RClass), true
	case *RakeFileListVal:
		return vm.consts["Rake::FileList"].(*RClass), true
	case *CapServerVal:
		return vm.consts["Capistrano::Server"].(*RClass), true
	case *CapSessionVal:
		return vm.consts["Capistrano::Session"].(*RClass), true
	case *CapTaskVal:
		return vm.consts["Capistrano::Task"].(*RClass), true
	case *CapBackendVal:
		return vm.consts["Capistrano::TestBackend"].(*RClass), true
	case *BundlerLockfile:
		return vm.consts["Bundler::LockfileParser"].(*RClass), true
	case *BundlerSpec:
		return vm.consts["Bundler::LazySpecification"].(*RClass), true
	case *BundlerDependency:
		return vm.consts["Bundler::Dependency"].(*RClass), true
	case *BundlerGemfile:
		return vm.consts["Bundler::Dsl"].(*RClass), true
	case *BundlerIndex:
		return vm.consts["Bundler::Index"].(*RClass), true
	case *MinitestAssertionsBox:
		return vm.consts["Minitest::Assertions"].(*RClass), true
	case *MinitestResult:
		return vm.consts["Minitest::Result"].(*RClass), true
	case *MinitestMock:
		return vm.consts["Minitest::Mock"].(*RClass), true
	case *FaradayConnection:
		return vm.consts["Faraday::Connection"].(*RClass), true
	case *FaradayRequest:
		return vm.consts["Faraday::Request"].(*RClass), true
	case *FaradayResponse:
		return vm.consts["Faraday::Response"].(*RClass), true
	case *FaradayParams:
		return vm.consts["Faraday::Utils::ParamsHash"].(*RClass), true
	case *FaradayHeaders:
		return vm.consts["Faraday::Utils::Headers"].(*RClass), true
	case *CapybaraSession:
		// A Capybara::Session wrapper reports the class stamped on it at construction.
		return x.cls, true
	case *CapybaraNode:
		return x.cls, true
	case *PumaServer:
		return vm.consts["Puma::Server"].(*RClass), true
	case *PumaThreadPool:
		return vm.consts["Puma::ThreadPool"].(*RClass), true
	case *PumaConfiguration:
		return vm.consts["Puma::Configuration"].(*RClass), true
	case *PumaDSL:
		return vm.consts["Puma::DSL"].(*RClass), true
	case *WEBrickServer:
		return vm.consts["WEBrick::HTTPServer"].(*RClass), true
	case *WEBrickRequest:
		return vm.consts["WEBrick::HTTPRequest"].(*RClass), true
	case *WEBrickResponse:
		return vm.consts["WEBrick::HTTPResponse"].(*RClass), true
	case *WardenManager:
		return x.cls, true
	case *WardenProxy:
		return x.cls, true
	case *WardenStrategy:
		return x.cls, true
	case *OmniAuthBuilder:
		return x.cls, true
	case *OmniAuthConfig:
		return x.cls, true
	case *OmniAuthMockAuth:
		return x.cls, true
	case *OmniAuthStrategy:
		return x.cls, true
	case *OmniAuthHash:
		return x.cls, true
	case *ACRouteSet:
		return x.cls, true
	case *ACMapper:
		return x.cls, true
	case *ACParams:
		return x.cls, true
	case *ACRequest:
		return x.cls, true
	case *ACResponse:
		return x.cls, true
	case *WebMockStub:
		return x.cls, true
	case *ShrineStorage:
		// A Shrine::Storage::Memory / ::FileSystem backend reports the concrete
		// class stamped on it so its upload/open/exists?/delete/url dispatch.
		return x.cls, true
	case *ShrineUploader:
		// A Shrine.new(:key) uploader reports Shrine so its #upload / #storage_key
		// (defined on the Shrine class) dispatch.
		return x.cls, true
	case *ShrineUploadedFile:
		return x.cls, true
	case *ShrineAttacher:
		return x.cls, true
	}
	return nil, false
}

// gemArith is the operator fast path for IPAddr: ip + n / ip - n shift the
// address by a whole-number offset (the bitwise & | ~ combinators dispatch as
// methods), mirroring MRI's IPAddr#+ / IPAddr#-.
func gemArith(op bytecode.Op, a, b object.Value) (object.Value, bool) {
	if ai, ok := a.(*IPAddr); ok {
		return ipaddrOp(op, ai, b), true
	}
	return nil, false
}

// gemDispatchesArith reports the gem values whose arithmetic operator is a
// Ruby method that needs a live VM, so binaryOp sends it rather than falling
// into the numeric coercion path:
//   - a URI (only + is defined, resolving a reference; the binding's merge
//     needs the VM to wrap the result);
//   - a Money (+ - * over the go-ruby-money library, raising
//     Money::DifferentCurrencyError on a currency mismatch);
//   - a Google::Protobuf::RepeatedField (+ is a new list: the receiver's
//     elements followed by the other list's);
//   - an ActiveSupport::SafeBuffer (+ is a new html-safe buffer with the right
//     operand escaped-unless-safe, so a view can build `f.label + f.text_field`).
func gemDispatchesArith(a object.Value) bool {
	switch a.(type) {
	case *URI, *Money, *ProtobufRepeatedField, *SafeBufferVal:
		return true
	}
	return false
}

// gemNegate is unary minus for Money: it negates the amount via go-ruby-money.
func gemNegate(v object.Value) (object.Value, bool) {
	if n, ok := v.(*Money); ok {
		return &Money{m: n.m.Neg()}, true
	}
	return nil, false
}

// gemValueEqual is value equality for IPAddr and URI.
func gemValueEqual(a, b object.Value) (bool, bool) {
	switch av := a.(type) {
	case *IPAddr:
		bv, ok := b.(*IPAddr)
		return ok && av.ip.Eql(bv.ip), true
	case *URI:
		return uriEqual(av, b), true
	}
	return false, false
}

// gemHasCustomEq reports the gem value types whose == compares by value, so it
// must be dispatched rather than fall to pointer identity:
//   - BCrypt::Password (compares a candidate secret against the stored hash);
//   - Money (fractional amount and currency) and Money::Currency (by id);
//   - Dry::Struct (attributes, via go-ruby-dry-struct's Struct.Eql);
//   - Google::Protobuf messages and their RepeatedField / Map containers
//     (proto.Equal / element-wise, through go-ruby-protobuf);
//   - SemanticPuppet::Version (SemVer precedence, via go-ruby-semantic-puppet);
//   - Rails::StringInquirer and Rails::EnvironmentInquirer (the wrapped
//     string: env == "production").
func gemHasCustomEq(v object.Value) bool {
	switch v.(type) {
	case *BCryptPassword, *Money, *Currency, *DryStruct,
		*ProtobufMessage, *ProtobufRepeatedField, *ProtobufMap,
		*SemVerObj, *StringInquirerVal, *RailsEnvVal:
		return true
	}
	return false
}

// ivarStoreOfGem is ivarStoreOf's half for the gem value types that carry their
// own instance variables (a Sinatra handler self, an ActionView context).
func ivarStoreOfGem(self object.Value) (ivarStore, bool) {
	switch o := self.(type) {
	case *SinatraCtx:
		if o.ivars == nil {
			o.ivars = map[string]object.Value{}
		}
		return ivarStore{tbl: o.ivars}, true
	case *ActionViewBase:
		if o.ivars == nil {
			o.ivars = map[string]object.Value{}
		}
		return ivarStore{tbl: o.ivars}, true
	}
	return ivarStore{}, false
}
