module github.com/go-embedded-ruby/ruby

go 1.27.2

require (
	github.com/alicebob/miniredis/v2 v2.41.0
	github.com/beevik/etree v1.8.1
	github.com/dolthub/go-mysql-server v0.20.0
	github.com/glauth/ldap v0.0.0-20260718202943-34c5f9b3cbf1
	github.com/go-commonmark/commonmark v0.1.0
	github.com/go-composites/bag v0.0.0-20261109012049-f38664b4de75
	github.com/go-composites/result v0.0.0-20261106020326-f6bf56cd3d3f
	github.com/go-composites/time v0.0.0-20261108010550-a718086f3708
	github.com/go-fft/fft v0.49.0
	github.com/go-images/images v0.3.1
	github.com/go-kramdown/kramdown v0.3.1
	github.com/go-liquid/liquid v0.2.0
	github.com/go-mustache/mustache v0.1.0
	github.com/go-ndarray/ndarray v0.46.0
	github.com/go-nokogiri/nokogiri v0.1.0
	github.com/go-numerics/integrate v0.3.0
	github.com/go-numerics/optimize v0.9.0
	github.com/go-numerics/special v0.22.1
	github.com/go-rouge/rouge v0.9.0
	github.com/go-ruby-aasm/aasm v0.0.0-20261112120312-5881a5b29ae5
	github.com/go-ruby-abbrev/abbrev v0.0.0-20261120122511-b13d94f10000
	github.com/go-ruby-acme/acme v0.0.0-20261113114849-d2c901ca2519
	github.com/go-ruby-actioncable/actioncable v0.0.0-20261120122639-1c576b90a601
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20261112120704-dc450d929f79
	github.com/go-ruby-actionpack/actionpack v0.0.0-20261112120748-6d17cb4a6d31
	github.com/go-ruby-actionview/actionview v0.0.0-20261119122751-54a05f187bfb
	github.com/go-ruby-activejob/activejob v0.0.0-20261112120920-1cd1eba8b892
	github.com/go-ruby-activeldap/activeldap v0.0.0-20261112121003-649da205f984
	github.com/go-ruby-activemodel/activemodel v0.0.0-20261120123004-df054b13eb24
	github.com/go-ruby-activerecord/activerecord v0.0.0-20261112121140-c51f822647db
	github.com/go-ruby-activestorage/activestorage v0.0.0-20261120123113-71aa32ee726d
	github.com/go-ruby-activesupport/activesupport v0.0.0-20261112121304-bd27def94727
	github.com/go-ruby-addressable/addressable v0.0.0-20261120123219-d9f926327809
	github.com/go-ruby-age/age v0.0.0-20261112121428-b7c3e254a0ed
	github.com/go-ruby-arrow/arrow v0.0.0-20261118124049-88ffe298667e
	github.com/go-ruby-async/async v0.0.0-20261112121551-4b069a9b32cb
	github.com/go-ruby-augeas/augeas v0.0.0-20261101004057-d24fe360fc60
	github.com/go-ruby-base64/base64 v0.0.0-20261120123451-e1ec410ae9f1
	github.com/go-ruby-bbolt/bbolt v0.0.0-20261112121810-33638ae59708
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20261120123607-c29aad4b43b1
	github.com/go-ruby-benchmark/benchmark v0.0.0-20261120123644-9af6017632c1
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20261120123733-a2f59fa0cc30
	github.com/go-ruby-bleve/bleve v0.0.0-20261126133642-48d3e961141e
	github.com/go-ruby-builder/builder v0.0.0-20261120123851-f5dd0389ac92
	github.com/go-ruby-bundler/bundler v0.0.0-20261120123931-200f5b283ce1
	github.com/go-ruby-cancancan/cancancan v0.0.0-20261120124010-a9f5f5a77aba
	github.com/go-ruby-capistrano/capistrano v0.0.0-20261101004052-9d2546820776
	github.com/go-ruby-capybara/capybara v0.0.0-20261119124200-4f952683f6aa
	github.com/go-ruby-cgi/cgi v0.0.0-20261120124139-2a32323a7962
	github.com/go-ruby-chronic/chronic v0.1.0
	github.com/go-ruby-cmath/cmath v0.0.0-20261120124305-e720bdd11cb7
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20261120124458-401e00601800
	github.com/go-ruby-confd/confd v0.0.0-20261101004550-824a5f6bd320
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20261120124607-a61cdbe9fc07
	github.com/go-ruby-csv/csv v0.0.0-20261120124643-ac321959c19b
	github.com/go-ruby-date/date v0.0.0-20261120124731-35ff415960a5
	github.com/go-ruby-deep-merge/deep-merge v0.0.0-20261101004909-ee8fdcad09f4
	github.com/go-ruby-devise/devise v0.0.0-20261120124901-a5bfb8644e88
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20261120124939-9b3f63ae12da
	github.com/go-ruby-digest/digest v0.0.0-20261120125031-00f79aeb1981
	github.com/go-ruby-dotenv/dotenv v0.0.0-20261120125157-8536cdcf231e
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20261120125240-57a68a84c98d
	github.com/go-ruby-dry-types/dry-types v0.0.0-20261120125317-ad41c3216642
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20261120125354-6efa772d1daa
	github.com/go-ruby-erb/erb v0.0.0-20261120125501-ef69577d32b6
	github.com/go-ruby-erubi/erubi v0.0.0-20261120125539-322059a146c6
	github.com/go-ruby-etcd/etcd v0.0.0-20261123142234-4944a27a2518
	github.com/go-ruby-excon/excon v0.0.0-20261120125721-65f749aae357
	github.com/go-ruby-facter/facter v0.0.0-20261101004324-0aeb0bd8682a
	github.com/go-ruby-factory-bot/factory-bot v0.0.0-20261112124821-4fd26739373f
	github.com/go-ruby-faker/faker v0.0.0-20261120125855-6b3d8db37a68
	github.com/go-ruby-faraday/faraday v0.0.0-20261120125932-a0f03bc0826c
	github.com/go-ruby-fast-gettext/fast-gettext v0.0.0-20261101004212-5b58b8364318
	github.com/go-ruby-find/find v0.0.0-20261120130114-d91f2ac6be6e
	github.com/go-ruby-format/format v0.0.0-20261120130156-d6578704b157
	github.com/go-ruby-friendly-id/friendly-id v0.0.0-20261112125310-31c8f3938678
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20261120130329-0083a78fd6ca
	github.com/go-ruby-grape/grape v0.0.0-20261120130411-677a75afeffa
	github.com/go-ruby-graphql/graphql v0.0.0-20261112125616-140738b585b3
	github.com/go-ruby-grpc/grpc v0.0.0-20261112125702-6738fb3cefff
	github.com/go-ruby-haml/haml v0.0.0-20261120130545-ae08e4f532b0
	github.com/go-ruby-hanami/hanami v0.0.0-20261120130623-d108f4162409
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20261112125917-7c338c14fb35
	github.com/go-ruby-hiera/hiera v0.0.0-20261101004350-3875ea9ddb54
	github.com/go-ruby-hocon/hocon v0.0.0-20261101004122-32003736fc94
	github.com/go-ruby-http/http v0.0.0-20261120130900-2f99b153c7ed
	github.com/go-ruby-httparty/httparty v0.0.0-20261120130935-79f33ba035dd
	github.com/go-ruby-i18n/i18n v0.0.0-20261120131011-e09a88ca15ae
	github.com/go-ruby-images/images v0.0.0-20261124150707-6359237bee78
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20261120131114-bd5fe2ac9796
	github.com/go-ruby-irb/irb v0.1.0
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20261120131234-77e306f1786e
	github.com/go-ruby-jekyll/jekyll v0.0.0-20261101005115-9c567fe43b9c
	github.com/go-ruby-json/json v0.0.0-20261120131348-bbb4a76a6e46
	github.com/go-ruby-jwt/jwt v0.0.0-20261120131425-e95c3330658d
	github.com/go-ruby-kafka/kafka v0.0.0-20261112131028-746992217367
	github.com/go-ruby-kaminari/kaminari v0.0.0-20261112131117-45135cb07ecd
	github.com/go-ruby-ldap/ldap v0.0.0-20261112131319-bab43b56dd64
	github.com/go-ruby-logger/logger v0.0.0-20261120131753-e25c8135af4e
	github.com/go-ruby-mail/mail v0.0.0-20261120131831-8b420ce87047
	github.com/go-ruby-marshal/marshal v0.0.0-20261101003159-51749bab9a2c
	github.com/go-ruby-matrix/matrix v0.0.0-20261120131940-59bdf627cf31
	github.com/go-ruby-mime-types/mime-types v0.0.0-20261120132022-bde94327b61d
	github.com/go-ruby-minitest/minitest v0.0.0-20261120132059-e96b16275a18
	github.com/go-ruby-money/money v0.0.0-20261120132136-7dde45b10a7c
	github.com/go-ruby-mongodb/mongodb v0.0.0-20261121122925-2776943da265
	github.com/go-ruby-msgpack/msgpack v0.0.0-20261120132247-0b4c7d4b65c6
	github.com/go-ruby-multi-json/multi-json v0.0.0-20261101004414-f95acb236825
	github.com/go-ruby-mysql/mysql v0.0.0-20261119132856-ec79f958dc33
	github.com/go-ruby-nats/nats v0.0.0-20261119132948-a0d976bffe06
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20261120132531-bbaeb4a791ec
	github.com/go-ruby-net-http/net-http v0.0.0-20261120132608-7b4c8644b203
	github.com/go-ruby-net-imap/net-imap v0.0.0-20261120132646-a34d8a2839a6
	github.com/go-ruby-net-pop/net-pop v0.0.0-20261120132723-4b54ee6687ca
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20261120132832-ff381b9b4c5b
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20261120132908-80759947de42
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20261120133027-70d15b53cd6a
	github.com/go-ruby-observer/observer v0.0.0-20261101003153-d7b712b2718f
	github.com/go-ruby-oidc/oidc v0.0.0-20261112133416-d458534dcb74
	github.com/go-ruby-omniauth/omniauth v0.0.0-20261112133503-260cd89294f3
	github.com/go-ruby-openbao/openbao v0.0.0-20261112133552-9af7a6e4a3e0
	github.com/go-ruby-openstack/openstack v0.0.0-20261101005324-eefe1e6899fa
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20261112133826-594ebea78b0f
	github.com/go-ruby-opentype/opentype v0.4.0
	github.com/go-ruby-optparse/optparse v0.0.0-20261120133515-3ac567417264
	github.com/go-ruby-ostruct/ostruct v0.0.0-20261101003623-ebd9f40141a6
	github.com/go-ruby-pagy/pagy v0.0.0-20261112134151-5d8db12c2c97
	github.com/go-ruby-paper-trail/paper-trail v0.0.0-20261112134236-47014d80e2a3
	github.com/go-ruby-parquet/parquet v0.0.0-20261112134342-5a9aa973f1e4
	github.com/go-ruby-parser/parser v0.14.0
	github.com/go-ruby-pathname/pathname v0.0.0-20261120133818-158a37de5705
	github.com/go-ruby-pg/pg v0.0.0-20261120133856-b9d18b8ebd57
	github.com/go-ruby-prawn/prawn v0.0.0-20261119134625-241ad0be4f73
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20261120134001-526e6f30a570
	github.com/go-ruby-prime/prime v0.0.0-20261120134039-66a878ef49b0
	github.com/go-ruby-protobuf/protobuf v0.0.0-20261112134930-0850612700d7
	github.com/go-ruby-pstore/pstore v0.0.0-20261120134149-9b9f80276c2f
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20261120134227-e89406c0b537
	github.com/go-ruby-puma/puma v0.0.0-20261112135151-3bef13055669
	github.com/go-ruby-pundit/pundit v0.0.0-20261120134332-6daa31cec832
	github.com/go-ruby-puppet-resource-api/puppet-resource-api v0.0.0-20261101004844-67bd6b716543
	github.com/go-ruby-puppet/puppet v0.0.0-20261101004527-7d45b712074b
	github.com/go-ruby-racc/racc v0.0.0-20261120134515-df126ed931c4
	github.com/go-ruby-rack/rack v0.0.0-20261120134551-41afc7e2d968
	github.com/go-ruby-rails/rails v0.0.0-20261112135738-1b45d0e3bc32
	github.com/go-ruby-railties/railties v0.0.0-20261120134730-07869715dc39
	github.com/go-ruby-rake/rake v0.0.0-20261120134813-3a3e8205b7cd
	github.com/go-ruby-ransack/ransack v0.0.0-20261112140012-3cc5f4e2ff52
	github.com/go-ruby-rdoc/rdoc v0.0.0-20261120134951-21fe25b6f54f
	github.com/go-ruby-redis/redis v0.0.0-20261120135106-143338fc1580
	github.com/go-ruby-regexp/regexp v0.2.1
	github.com/go-ruby-reline/reline v0.1.0
	github.com/go-ruby-resolv/resolv v0.0.0-20261120135302-543397e544c1
	github.com/go-ruby-resque/resque v0.0.0-20261112140659-c58ff8ebf53c
	github.com/go-ruby-rexml/rexml v0.0.0-20261120135416-bb911eb55fd5
	github.com/go-ruby-roda/roda v0.0.0-20261120135457-77be3c24211f
	github.com/go-ruby-rolify/rolify v0.0.0-20261112140927-3f65648f9f02
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20261120135647-2072db6c9b9f
	github.com/go-ruby-rspec/rspec v0.0.0-20261120135725-3a5bcabe90a6
	github.com/go-ruby-rss/rss v0.0.0-20261120135804-b23b1fcd06a5
	github.com/go-ruby-rubocop/rubocop v0.0.0-20261112141353-7be41eb24a3c
	github.com/go-ruby-rubygems/rubygems v0.0.0-20261120135928-14b8d25efd74
	github.com/go-ruby-saml/saml v0.0.0-20261112141550-3a5b27cf4842
	github.com/go-ruby-sass/sass v0.0.0-20261004225654-93da1ddfb811
	github.com/go-ruby-scanf/scanf v0.0.0-20261120140112-dff98dce5c65
	github.com/go-ruby-securerandom/securerandom v0.0.0-20261120140202-ced8edfdf575
	github.com/go-ruby-semantic-puppet/semantic-puppet v0.0.0-20261101004754-6086b082f47d
	github.com/go-ruby-sequel/sequel v0.0.0-20261120140312-f49f2bcdcb9a
	github.com/go-ruby-shellwords/shellwords v0.0.0-20261120140431-b36034b28295
	github.com/go-ruby-shrine/shrine v0.0.0-20261112142244-126e2525f4c0
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20261120140537-2293ab7566d0
	github.com/go-ruby-simplecov/simplecov v0.0.0-20261112142414-b8e786c7db8d
	github.com/go-ruby-sinatra/sinatra v0.0.0-20261120140649-dc8c092b7f28
	github.com/go-ruby-slim/slim v0.0.0-20261120140729-5df7b75a32c6
	github.com/go-ruby-sodium/sodium v0.0.0-20261113134419-19cdd8cd5d22
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20261127143243-ea4fee0d3666
	github.com/go-ruby-strscan/strscan v0.0.0-20261120141222-a5a0c3d58cee
	github.com/go-ruby-thor/thor v0.0.0-20261120141339-0d625c44a82f
	github.com/go-ruby-timecop/timecop v0.0.0-20261112143559-8c56a7ade067
	github.com/go-ruby-toml/toml v0.0.0-20261120141513-4b8eecc7e50e
	github.com/go-ruby-tsort/tsort v0.0.0-20261120141558-aa9421cc3cdf
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20261120141637-658dc8af58fd
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20261120141717-cd78c857a61b
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.1.0
	github.com/go-ruby-uri/uri v0.0.0-20261120141837-35ea11d6fb40
	github.com/go-ruby-vcr/vcr v0.0.0-20261112144136-9178d8b11e2b
	github.com/go-ruby-warden/warden v0.0.0-20261120141942-0e6aa72a2276
	github.com/go-ruby-webauthn/webauthn v0.0.0-20261112144320-03ccd0b1afd2
	github.com/go-ruby-webmock/webmock v0.0.0-20261112144408-f7d936a1000b
	github.com/go-ruby-webrick/webrick v0.0.0-20261120142119-8ca8593e5340
	github.com/go-ruby-widgets/mvvm v0.3.0
	github.com/go-ruby-widgets/tui v0.5.0
	github.com/go-ruby-widgets/widgets v0.13.0
	github.com/go-ruby-yaml/yaml v0.1.0
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20261120142422-a2b037495afa
	github.com/go-ruby-zlib/zlib v0.0.0-20261127144353-be6f409c5f49
	github.com/go-statistics/stats v0.1.1
	github.com/go-webauthn/webauthn v0.18.2
	github.com/go-xslt/xslt v0.1.0
	github.com/nats-io/nats-server/v2 v2.15.3
	github.com/redis/go-redis/v9 v9.24.1
	github.com/russellhaering/goxmldsig v1.6.1
	github.com/sirupsen/logrus v1.10.2
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20261127044302-899cc3164d3c
	go.etcd.io/etcd/server/v3 v3.7.4
	golang.org/x/sys v0.50.0
	golang.org/x/text v0.44.0
)

require (
	filippo.io/age v1.3.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.14.5 // indirect
	github.com/abtreece/confd v0.41.2 // indirect
	github.com/andybalholm/brotli v1.3.0 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.9.0-default-no-op // indirect
	github.com/apache/arrow-go/v18 v18.9.0 // indirect
	github.com/apache/thrift v0.25.0 // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/aws/aws-sdk-go-v2 v1.41.7 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.17 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.16 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/acm v1.38.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.57.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.11.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssm v1.68.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.42.1 // indirect
	github.com/aws/smithy-go v1.25.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.2 // indirect
	github.com/blevesearch/bleve/v2 v2.6.4 // indirect
	github.com/blevesearch/bleve_index_api v1.4.1 // indirect
	github.com/blevesearch/geo v0.2.8 // indirect
	github.com/blevesearch/go-faiss v1.1.5 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.4.11 // indirect
	github.com/blevesearch/segment v0.9.1 // indirect
	github.com/blevesearch/snowballstem v0.9.0 // indirect
	github.com/blevesearch/upsidedown_store_api v1.0.2 // indirect
	github.com/blevesearch/vellum v1.2.1 // indirect
	github.com/blevesearch/zapx/v11 v11.4.3 // indirect
	github.com/blevesearch/zapx/v12 v12.4.3 // indirect
	github.com/blevesearch/zapx/v13 v13.4.3 // indirect
	github.com/blevesearch/zapx/v14 v14.4.3 // indirect
	github.com/blevesearch/zapx/v15 v15.4.3 // indirect
	github.com/blevesearch/zapx/v16 v16.3.4 // indirect
	github.com/blevesearch/zapx/v17 v17.2.5 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/crewjam/saml v0.6.3 // indirect
	github.com/dolthub/flatbuffers/v23 v23.3.3-dh.2 // indirect
	github.com/dolthub/go-icu-regex v0.0.0-20250327004329-6799764f2dad // indirect
	github.com/dolthub/jsonpath v0.0.2-0.20240227200619-19675ab05c71 // indirect
	github.com/dolthub/vitess v0.0.0-20250512224608-8fb9c6ea092c // indirect
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8 // indirect
	github.com/go-augeas/augeas v0.0.0-20260830115849-a0db83a6594a // indirect
	github.com/go-blas/blas v0.2.1 // indirect
	github.com/go-composites/array v0.0.0-20261108005945-6e532871dc66 // indirect
	github.com/go-composites/error v0.0.0-20261104015158-766d8cb306ef // indirect
	github.com/go-composites/null v0.0.0-20261031224807-35430d41335f // indirect
	github.com/go-crdt/collab v0.74.0 // indirect
	github.com/go-crdt/crdt v0.55.0 // indirect
	github.com/go-datetime/dates v0.2.0 // indirect
	github.com/go-encryptions/unixcrypt v0.1.0 // indirect
	github.com/go-facter/facter v0.0.0-20260830120958-454b72e642ab // indirect
	github.com/go-gfx/gfx v0.37.0 // indirect
	github.com/go-hiera/hiera v0.0.0-20260830144306-f9304f6bec92 // indirect
	github.com/go-hocon/hocon v0.0.0-20260831114632-08e716b40e6d // indirect
	github.com/go-icons/iconoir v0.2.0 // indirect
	github.com/go-images/gif v0.2.0 // indirect
	github.com/go-images/jpeg v0.3.0 // indirect
	github.com/go-images/jpeg2000 v0.14.0 // indirect
	github.com/go-images/png v0.2.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-ldap/ldap/v3 v3.4.15 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-opentype/fonts v0.10.0 // indirect
	github.com/go-opentype/opentype v0.22.0 // indirect
	github.com/go-opentype/shape v0.7.0 // indirect
	github.com/go-pcore/pcore v0.0.0-20260831114716-f9c3e7f59eaa // indirect
	github.com/go-puppet/puppet v0.0.0-20260918012035-fc6b0424cdbd // indirect
	github.com/go-regexp/engine v0.4.2 // indirect
	github.com/go-richdoc/richdoc v0.4.0 // indirect
	github.com/go-ruby-fast-gettext-locale/fast-gettext-locale v0.0.0-20260825110154-a53e0e3a41a7 // indirect
	github.com/go-scss/scss v0.0.0-20260905061546-39932e01faa4 // indirect
	github.com/go-simd/adler32 v0.2.0 // indirect
	github.com/go-simd/base64 v0.1.0 // indirect
	github.com/go-simd/crc32 v0.1.0 // indirect
	github.com/go-simd/floats v0.40.0 // indirect
	github.com/go-simd/hex v0.0.0-20260903220024-a8d22a843218 // indirect
	github.com/go-simd/highway v0.0.14 // indirect
	github.com/go-sparse/sparse v0.3.0 // indirect
	github.com/go-sql-driver/mysql v1.10.2 // indirect
	github.com/go-typeset/bidi v0.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/go-widgets/mvvm v0.9.0 // indirect
	github.com/go-widgets/painter v0.13.0 // indirect
	github.com/go-widgets/toolkit v0.321.2 // indirect
	github.com/go-widgets/tui v0.61.0 // indirect
	github.com/go-zookeeper/zk v1.0.4 // indirect
	github.com/goccy/go-json v0.12.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gophercloud/gophercloud/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/graphql-go/graphql v0.8.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.1.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/hashicorp/consul/api v1.34.2 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.2.0 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/hashicorp/vault/api v1.23.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.20.5 // indirect
	github.com/klauspost/cpuid/v2 v2.4.2 // indirect
	github.com/lestrrat-go/strftime v1.0.4 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.3 // indirect
	github.com/nats-io/nats.go v1.56.0 // indirect
	github.com/nats-io/nkeys v0.4.17 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tannevaled/gobig2 v0.2.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/twmb/franz-go v1.22.3 // indirect
	github.com/twmb/franz-go/pkg/kadm v1.20.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.15.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/scram v1.4.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xiang90/probing v0.0.0-20221125231312-a49e3df8f510 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	go.etcd.io/etcd/api/v3 v3.7.4 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.4 // indirect
	go.etcd.io/etcd/client/v3 v3.7.4 // indirect
	go.etcd.io/etcd/pkg/v3 v3.7.4 // indirect
	go.etcd.io/raft/v3 v3.7.0 // indirect
	go.mongodb.org/mongo-driver/v2 v2.10.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.45.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.45.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/telemetry v0.0.0-20261009151841-97e8ea77aa2a // indirect
	golang.org/x/term v0.48.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	google.golang.org/grpc v1.85.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.63.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
