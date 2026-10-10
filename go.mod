module github.com/go-embedded-ruby/ruby

go 1.27.2

require (
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/beevik/etree v1.8.1
	github.com/dolthub/go-mysql-server v0.20.0
	github.com/glauth/ldap v0.0.0-20260718202943-34c5f9b3cbf1
	github.com/go-commonmark/commonmark v0.1.0
	github.com/go-composites/bag v0.0.0-20261009013603-e5caafb0556e
	github.com/go-composites/result v0.0.0-20261006020718-14f01380a20a
	github.com/go-composites/time v0.0.0-20261008013532-6f53dc2c1b2a
	github.com/go-fft/fft v0.23.0
	github.com/go-images/images v0.1.0
	github.com/go-kramdown/kramdown v0.3.0
	github.com/go-liquid/liquid v0.2.0
	github.com/go-mustache/mustache v0.1.0
	github.com/go-ndarray/ndarray v0.9.1
	github.com/go-nokogiri/nokogiri v0.1.0
	github.com/go-rouge/rouge v0.4.0
	github.com/go-ruby-aasm/aasm v0.0.0-20261010102136-8c21422351e3
	github.com/go-ruby-abbrev/abbrev v0.0.0-20261010102213-4afde4be814a
	github.com/go-ruby-acme/acme v0.0.0-20261010102310-cc8d39b40408
	github.com/go-ruby-actioncable/actioncable v0.0.0-20261010102347-a7a151e82412
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20261010102422-5ca4013197eb
	github.com/go-ruby-actionpack/actionpack v0.0.0-20261010102452-67a1d71146a4
	github.com/go-ruby-actionview/actionview v0.0.0-20261010102525-abb42235d8f5
	github.com/go-ruby-activejob/activejob v0.0.0-20261010102558-39f8abf74db5
	github.com/go-ruby-activeldap/activeldap v0.0.0-20261010102626-5ec0ba45ae00
	github.com/go-ruby-activemodel/activemodel v0.0.0-20261010102655-05757392ef36
	github.com/go-ruby-activerecord/activerecord v0.0.0-20261010102724-328a496aa069
	github.com/go-ruby-activestorage/activestorage v0.0.0-20261010102752-d7cd3b1c154f
	github.com/go-ruby-activesupport/activesupport v0.0.0-20261010102820-3ef4fda78f1a
	github.com/go-ruby-addressable/addressable v0.0.0-20261010102847-be9cd896cab0
	github.com/go-ruby-age/age v0.0.0-20261010102915-2176e0838ff8
	github.com/go-ruby-arrow/arrow v0.0.0-20261010102944-86c08abf65f1
	github.com/go-ruby-async/async v0.0.0-20261010103014-1a287de056fd
	github.com/go-ruby-augeas/augeas v0.0.0-20261004231654-c1fd821e5edd
	github.com/go-ruby-base64/base64 v0.0.0-20261010103111-e0ad51879f5e
	github.com/go-ruby-bbolt/bbolt v0.0.0-20261010103205-a2245cf24822
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20261010103234-1bf30216fb1a
	github.com/go-ruby-benchmark/benchmark v0.0.0-20261010103308-a232c405ab1c
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20261010103402-a6236f9748cd
	github.com/go-ruby-bleve/bleve v0.0.0-20261010103454-e15c376d22ff
	github.com/go-ruby-builder/builder v0.0.0-20261010103527-cfcd95577945
	github.com/go-ruby-bundler/bundler v0.0.0-20261010103600-4dfe9f6f8513
	github.com/go-ruby-cancancan/cancancan v0.0.0-20261010103629-b4942069e245
	github.com/go-ruby-capistrano/capistrano v0.0.0-20261007112325-38fd38d55f7e
	github.com/go-ruby-capybara/capybara v0.0.0-20261010103723-c6dc6ffa9496
	github.com/go-ruby-cgi/cgi v0.0.0-20261010103755-685d7ad9f2b2
	github.com/go-ruby-chronic/chronic v0.0.0-20261010103852-413ed5ad9c6d
	github.com/go-ruby-cmath/cmath v0.0.0-20261010103922-9168bf461614
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20261010104136-5a6eb39c405b
	github.com/go-ruby-confd/confd v0.0.0-20261007203739-c1768190cd28
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20261010104229-bbdbde72f43d
	github.com/go-ruby-csv/csv v0.0.0-20261010104258-a00c85221f6a
	github.com/go-ruby-date/date v0.0.0-20261010104352-78655621fab6
	github.com/go-ruby-deep-merge/deep-merge v0.0.0-20261005015456-70e11765a8c6
	github.com/go-ruby-devise/devise v0.0.0-20261010104540-0dba8360179b
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20261010104611-cf32ac484a6f
	github.com/go-ruby-digest/digest v0.0.0-20261010104705-a88e917ce22c
	github.com/go-ruby-dotenv/dotenv v0.0.0-20261010104838-2fc661c789af
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20261010104912-a3561c6c6140
	github.com/go-ruby-dry-types/dry-types v0.0.0-20261010104941-c6395db8880d
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20261010105009-2b215a2fd184
	github.com/go-ruby-erb/erb v0.0.0-20261010105106-490d9be5cd50
	github.com/go-ruby-erubi/erubi v0.0.0-20261010105137-10d1b52e1e9f
	github.com/go-ruby-etcd/etcd v0.0.0-20261010105214-2d314cc0cdce
	github.com/go-ruby-excon/excon v0.0.0-20261010105245-35ef64cb98f1
	github.com/go-ruby-facter/facter v0.0.0-20261005015634-ef35c9f26404
	github.com/go-ruby-factory-bot/factory-bot v0.0.0-20261010105339-e8de9dfd887e
	github.com/go-ruby-faker/faker v0.0.0-20261010105408-b888a558ece2
	github.com/go-ruby-faraday/faraday v0.0.0-20261010105438-664ce05be584
	github.com/go-ruby-fast-gettext/fast-gettext v0.0.0-20261005015533-b590451bebe3
	github.com/go-ruby-find/find v0.0.0-20261010105556-6cc4611c7a4a
	github.com/go-ruby-format/format v0.0.0-20261010105630-9cf0897fa094
	github.com/go-ruby-friendly-id/friendly-id v0.0.0-20261010105659-2cde0890af69
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20261010105805-a53b13fce56d
	github.com/go-ruby-grape/grape v0.0.0-20261010105836-b5c762782e99
	github.com/go-ruby-graphql/graphql v0.0.0-20261010105906-e6fa86187ecd
	github.com/go-ruby-grpc/grpc v0.0.0-20261010105940-dcc1e81a01ab
	github.com/go-ruby-haml/haml v0.0.0-20261010110016-9b77a0ef3d73
	github.com/go-ruby-hanami/hanami v0.0.0-20261010110046-e4e9b8a3c144
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20261010110117-af9445cb261a
	github.com/go-ruby-hiera/hiera v0.0.0-20261004230518-d452a2babd8f
	github.com/go-ruby-hocon/hocon v0.0.0-20261005015557-1dc1c78df5c4
	github.com/go-ruby-http/http v0.0.0-20261010110301-e77259137dcc
	github.com/go-ruby-httparty/httparty v0.0.0-20261010110329-39c524c8f747
	github.com/go-ruby-i18n/i18n v0.0.0-20261010110359-fbedd63bcfed
	github.com/go-ruby-images/images v0.0.0-20261010110431-1c9f5356a99b
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20261010110509-96f92f7569a2
	github.com/go-ruby-irb/irb v0.0.0-20261010110538-b28b9d661e70
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20261010110608-b5fb3bfd9e39
	github.com/go-ruby-jekyll/jekyll v0.0.0-20261005015353-c1dae98a78b2
	github.com/go-ruby-json/json v0.0.0-20261010110711-bacc696774dc
	github.com/go-ruby-jwt/jwt v0.0.0-20261010110750-d09fb5d03a68
	github.com/go-ruby-kafka/kafka v0.0.0-20261010110823-d87e390ec38b
	github.com/go-ruby-kaminari/kaminari v0.0.0-20261010110852-b45988f64e6f
	github.com/go-ruby-ldap/ldap v0.0.0-20261010111001-c0270d3f7606
	github.com/go-ruby-logger/logger v0.0.0-20261007112716-4a19b4fb21a1
	github.com/go-ruby-mail/mail v0.0.0-20261010111122-8ca230c9a32a
	github.com/go-ruby-marshal/marshal v0.0.0-20261005011818-bc1c92af1c72
	github.com/go-ruby-matrix/matrix v0.0.0-20261010111220-ef9ae0a15839
	github.com/go-ruby-mime-types/mime-types v0.0.0-20261010111249-c28c3f2c255b
	github.com/go-ruby-minitest/minitest v0.0.0-20261010111322-65780074fffe
	github.com/go-ruby-money/money v0.0.0-20261010111351-efebbba95b6b
	github.com/go-ruby-mongodb/mongodb v0.0.0-20261010111421-7286bb39d86e
	github.com/go-ruby-msgpack/msgpack v0.0.0-20261010111451-14cd5b8efcaf
	github.com/go-ruby-multi-json/multi-json v0.0.0-20261004232942-6222f9e9d20a
	github.com/go-ruby-mysql/mysql v0.0.0-20261010111609-dc15d9505e89
	github.com/go-ruby-nats/nats v0.0.0-20261010111646-efc674539b89
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20261010111721-6d1aada8892f
	github.com/go-ruby-net-http/net-http v0.0.0-20261010111755-67a09daf512e
	github.com/go-ruby-net-imap/net-imap v0.0.0-20261010111824-0664f8d0b403
	github.com/go-ruby-net-pop/net-pop v0.0.0-20261010111854-0876af0ed3c8
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20261010111959-cf5736440708
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20261010112029-b6ee0a8a032c
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20261010112131-d51bfa5a71a6
	github.com/go-ruby-observer/observer v0.0.0-20261005011309-65255fb61e66
	github.com/go-ruby-oidc/oidc v0.0.0-20261010112300-cb559f246206
	github.com/go-ruby-omniauth/omniauth v0.0.0-20261010112334-3b9802ecfd31
	github.com/go-ruby-openbao/openbao v0.0.0-20261010112403-78d5a944a6d6
	github.com/go-ruby-openstack/openstack v0.0.0-20261007112329-2deacfff653b
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20261010112545-78fcb898cfed
	github.com/go-ruby-opentype/opentype v0.4.0
	github.com/go-ruby-optparse/optparse v0.0.0-20261010112649-09e1902da2f8
	github.com/go-ruby-ostruct/ostruct v0.0.0-20261004235033-c13e0df4bc07
	github.com/go-ruby-pagy/pagy v0.0.0-20261010112741-f00fab7ef2d5
	github.com/go-ruby-paper-trail/paper-trail v0.0.0-20261010112810-77890c999bd3
	github.com/go-ruby-parquet/parquet v0.0.0-20261010112906-8b6f3bfe91b6
	github.com/go-ruby-parser/parser v0.13.1
	github.com/go-ruby-pathname/pathname v0.0.0-20261010113006-ce829b8b52a7
	github.com/go-ruby-pg/pg v0.0.0-20261010113037-38091c29987a
	github.com/go-ruby-prawn/prawn v0.0.0-20261010113109-1cb80b1cc524
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20261010113142-58de5cc147ce
	github.com/go-ruby-prime/prime v0.0.0-20261010113216-271292744bc5
	github.com/go-ruby-protobuf/protobuf v0.0.0-20261010113245-09d220ca96f5
	github.com/go-ruby-pstore/pstore v0.0.0-20261010113320-82ba6b99bdec
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20261010113350-5c0567a457b3
	github.com/go-ruby-puma/puma v0.0.0-20261010113418-af0244892c10
	github.com/go-ruby-pundit/pundit v0.0.0-20261010113447-5fe5e72593f8
	github.com/go-ruby-puppet-resource-api/puppet-resource-api v0.0.0-20261005015508-7d09086584fa
	github.com/go-ruby-puppet/puppet v0.0.0-20261005015622-ea0115fdd1d0
	github.com/go-ruby-racc/racc v0.0.0-20261010113604-173ec528489f
	github.com/go-ruby-rack/rack v0.0.0-20261010113634-603a0ca4834a
	github.com/go-ruby-rails/rails v0.0.0-20261010113852-46a0f36b0cc1
	github.com/go-ruby-railties/railties v0.0.0-20261010113928-a67c3f0c7aec
	github.com/go-ruby-rake/rake v0.0.0-20261010113958-4bf54532fde6
	github.com/go-ruby-ransack/ransack v0.0.0-20261010114029-df7c81a9dd55
	github.com/go-ruby-rdoc/rdoc v0.0.0-20261010114135-88518962e54e
	github.com/go-ruby-redis/redis v0.0.0-20261010114228-a63906116d1e
	github.com/go-ruby-regexp/regexp v0.1.0
	github.com/go-ruby-reline/reline v0.0.0-20261010114334-a1b54830b8a4
	github.com/go-ruby-resolv/resolv v0.0.0-20261010114418-4b7242bf0b64
	github.com/go-ruby-resque/resque v0.0.0-20261010114451-180f642e3559
	github.com/go-ruby-rexml/rexml v0.0.0-20261010114525-a74626b88c38
	github.com/go-ruby-roda/roda v0.0.0-20261010114556-0be01a820c16
	github.com/go-ruby-rolify/rolify v0.0.0-20261010114626-8c707dd423d0
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20261010114724-beecc98601ae
	github.com/go-ruby-rspec/rspec v0.0.0-20261010114753-45baa94b8f6a
	github.com/go-ruby-rss/rss v0.0.0-20261010114824-40b4ed0c08e4
	github.com/go-ruby-rubocop/rubocop v0.0.0-20261010114857-0daebdc83006
	github.com/go-ruby-rubygems/rubygems v0.0.0-20261010114942-793f61b91494
	github.com/go-ruby-saml/saml v0.0.0-20261010115013-e0ee46170a17
	github.com/go-ruby-sass/sass v0.0.0-20261004225654-93da1ddfb811
	github.com/go-ruby-scanf/scanf v0.0.0-20261010115108-281dd6a5f51b
	github.com/go-ruby-securerandom/securerandom v0.0.0-20261010115151-608c170d6d22
	github.com/go-ruby-semantic-puppet/semantic-puppet v0.0.0-20261005015545-1d8a7ff64717
	github.com/go-ruby-sequel/sequel v0.0.0-20261010115242-36828f88e822
	github.com/go-ruby-shellwords/shellwords v0.0.0-20261010115348-955711fc6801
	github.com/go-ruby-shrine/shrine v0.0.0-20261010115417-87303fbb467b
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20261010115446-3e75fc3e6ca4
	github.com/go-ruby-simplecov/simplecov v0.0.0-20261010115519-0d297d415906
	github.com/go-ruby-sinatra/sinatra v0.0.0-20261010115553-1eeb022382b6
	github.com/go-ruby-slim/slim v0.0.0-20261010115622-9c3a529e6eb2
	github.com/go-ruby-sodium/sodium v0.0.0-20261010115650-2ed05b9c74a5
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20261010115723-f4b695d77bab
	github.com/go-ruby-strscan/strscan v0.0.0-20261010120026-d604c5754559
	github.com/go-ruby-thor/thor v0.0.0-20261010120123-3af2b1b51929
	github.com/go-ruby-timecop/timecop v0.0.0-20261010120219-8d89976e2a27
	github.com/go-ruby-toml/toml v0.0.0-20261010120249-766e470d160f
	github.com/go-ruby-tsort/tsort v0.0.0-20261010120320-a62a7093bfc6
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20261010120348-5b38e9abf0e8
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20261010120416-9d845dfc34c6
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.1.0
	github.com/go-ruby-uri/uri v0.0.0-20261010120517-15e79fd36f9e
	github.com/go-ruby-vcr/vcr v0.0.0-20261010120545-e9cac1b5b3c4
	github.com/go-ruby-warden/warden v0.0.0-20261010120614-22443f7b3aa2
	github.com/go-ruby-webauthn/webauthn v0.0.0-20261010120645-b0b0f456613c
	github.com/go-ruby-webmock/webmock v0.0.0-20261010120715-433943c19555
	github.com/go-ruby-webrick/webrick v0.0.0-20261010120749-ccbd1281ac22
	github.com/go-ruby-widgets/mvvm v0.3.0
	github.com/go-ruby-widgets/tui v0.5.0
	github.com/go-ruby-widgets/widgets v0.13.0
	github.com/go-ruby-yaml/yaml v0.1.0
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20261010121110-f32988427c23
	github.com/go-ruby-zlib/zlib v0.0.0-20261010121152-131deab75bb6
	github.com/go-webauthn/webauthn v0.18.2
	github.com/go-xslt/xslt v0.1.0
	github.com/nats-io/nats-server/v2 v2.15.1
	github.com/redis/go-redis/v9 v9.23.0
	github.com/russellhaering/goxmldsig v1.6.1
	github.com/sirupsen/logrus v1.10.2
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20261007040850-d3792b34935a
	go.etcd.io/etcd/server/v3 v3.7.2
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
)

require (
	filippo.io/age v1.3.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.14.5 // indirect
	github.com/abtreece/confd v0.41.2 // indirect
	github.com/ajroetker/go-highway v0.0.12 // indirect
	github.com/andybalholm/brotli v1.2.6 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.8.0-default-no-op // indirect
	github.com/apache/arrow-go/v18 v18.8.0 // indirect
	github.com/apache/thrift v0.24.0 // indirect
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
	github.com/blevesearch/bleve/v2 v2.6.1 // indirect
	github.com/blevesearch/bleve_index_api v1.4.1 // indirect
	github.com/blevesearch/geo v0.2.6 // indirect
	github.com/blevesearch/go-faiss v1.1.5 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.4.10 // indirect
	github.com/blevesearch/segment v0.9.1 // indirect
	github.com/blevesearch/snowballstem v0.9.0 // indirect
	github.com/blevesearch/upsidedown_store_api v1.0.2 // indirect
	github.com/blevesearch/vellum v1.2.0 // indirect
	github.com/blevesearch/zapx/v11 v11.4.3 // indirect
	github.com/blevesearch/zapx/v12 v12.4.3 // indirect
	github.com/blevesearch/zapx/v13 v13.4.3 // indirect
	github.com/blevesearch/zapx/v14 v14.4.3 // indirect
	github.com/blevesearch/zapx/v15 v15.4.3 // indirect
	github.com/blevesearch/zapx/v16 v16.3.4 // indirect
	github.com/blevesearch/zapx/v17 v17.2.3 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/crewjam/saml v0.5.1 // indirect
	github.com/dolthub/flatbuffers/v23 v23.3.3-dh.2 // indirect
	github.com/dolthub/go-icu-regex v0.0.0-20250327004329-6799764f2dad // indirect
	github.com/dolthub/jsonpath v0.0.2-0.20240227200619-19675ab05c71 // indirect
	github.com/dolthub/vitess v0.0.0-20250512224608-8fb9c6ea092c // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8 // indirect
	github.com/go-augeas/augeas v0.0.0-20260830115849-a0db83a6594a // indirect
	github.com/go-composites/array v0.0.0-20261008012822-b187ea4ffff3 // indirect
	github.com/go-composites/error v0.0.0-20261004233631-3186f2071cf7 // indirect
	github.com/go-composites/null v0.0.0-20261004234613-b811f56c1c66 // indirect
	github.com/go-crdt/collab v0.74.0 // indirect
	github.com/go-crdt/crdt v0.55.0 // indirect
	github.com/go-datetime/dates v0.2.0 // indirect
	github.com/go-encryptions/unixcrypt v0.1.0 // indirect
	github.com/go-facter/facter v0.0.0-20260830120958-454b72e642ab // indirect
	github.com/go-gfx/gfx v0.34.1 // indirect
	github.com/go-hiera/hiera v0.0.0-20260830144306-f9304f6bec92 // indirect
	github.com/go-hocon/hocon v0.0.0-20260831114632-08e716b40e6d // indirect
	github.com/go-icons/iconoir v0.2.0 // indirect
	github.com/go-images/gif v0.2.0 // indirect
	github.com/go-images/jpeg v0.3.0 // indirect
	github.com/go-images/jpeg2000 v0.13.2 // indirect
	github.com/go-images/png v0.2.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-ldap/ldap/v3 v3.4.15 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-opentype/fonts v0.10.0 // indirect
	github.com/go-opentype/opentype v0.15.0 // indirect
	github.com/go-opentype/shape v0.7.0 // indirect
	github.com/go-pcore/pcore v0.0.0-20260831114716-f9c3e7f59eaa // indirect
	github.com/go-puppet/puppet v0.0.0-20260918012035-fc6b0424cdbd // indirect
	github.com/go-regexp/engine v0.3.0 // indirect
	github.com/go-richdoc/richdoc v0.4.0 // indirect
	github.com/go-ruby-fast-gettext-locale/fast-gettext-locale v0.0.0-20260825110154-a53e0e3a41a7 // indirect
	github.com/go-scss/scss v0.0.0-20260905061546-39932e01faa4 // indirect
	github.com/go-simd/adler32 v0.1.0 // indirect
	github.com/go-simd/base64 v0.1.0 // indirect
	github.com/go-simd/crc32 v0.0.0-20260903220012-5f164e0e0487 // indirect
	github.com/go-simd/hex v0.0.0-20260903220024-a8d22a843218 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/go-typeset/bidi v0.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/go-widgets/mvvm v0.9.0 // indirect
	github.com/go-widgets/painter v0.13.0 // indirect
	github.com/go-widgets/toolkit v0.321.2 // indirect
	github.com/go-widgets/tui v0.61.0 // indirect
	github.com/go-zookeeper/zk v1.0.4 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
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
	github.com/klauspost/compress v1.20.1 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/lestrrat-go/strftime v1.0.4 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nats.go v1.54.0 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.30 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sergeymakinen/go-bmp v1.0.0 // indirect
	github.com/sergeymakinen/go-ico v1.0.0 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tannevaled/gobig2 v0.2.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/twmb/franz-go v1.22.1 // indirect
	github.com/twmb/franz-go/pkg/kadm v1.19.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.14.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xiang90/probing v0.0.0-20221125231312-a49e3df8f510 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	go.etcd.io/etcd/api/v3 v3.7.2 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.2 // indirect
	go.etcd.io/etcd/client/v3 v3.7.2 // indirect
	go.etcd.io/etcd/pkg/v3 v3.7.2 // indirect
	go.etcd.io/raft/v3 v3.7.0 // indirect
	go.mongodb.org/mongo-driver/v2 v2.9.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20260924152758-ed294f943157 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.60.1 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
