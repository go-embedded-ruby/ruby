module github.com/go-embedded-ruby/ruby

go 1.27.2

require (
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/beevik/etree v1.8.1
	github.com/dolthub/go-mysql-server v0.20.0
	github.com/glauth/ldap v0.0.0-20260718202943-34c5f9b3cbf1
	github.com/go-commonmark/commonmark v0.1.0
	github.com/go-composites/bag v0.0.0-20261017234052-8c0542744097
	github.com/go-composites/result v0.0.0-20261014005241-188da08131d5
	github.com/go-composites/time v0.0.0-20261016000833-3cfecd11ee53
	github.com/go-fft/fft v0.27.3
	github.com/go-images/images v0.1.0
	github.com/go-kramdown/kramdown v0.3.0
	github.com/go-liquid/liquid v0.2.0
	github.com/go-mustache/mustache v0.1.0
	github.com/go-ndarray/ndarray v0.18.0
	github.com/go-nokogiri/nokogiri v0.1.0
	github.com/go-numerics/integrate v0.2.0
	github.com/go-numerics/optimize v0.2.0
	github.com/go-numerics/special v0.2.0
	github.com/go-rouge/rouge v0.7.0
	github.com/go-ruby-aasm/aasm v0.0.0-20261022090456-a670e630b15f
	github.com/go-ruby-abbrev/abbrev v0.0.0-20261024083430-0217f0e6e411
	github.com/go-ruby-acme/acme v0.0.0-20261022090611-fc9da1bcd435
	github.com/go-ruby-actioncable/actioncable v0.0.0-20261024083550-7d5bdf862265
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20261024083627-a76c83a8a060
	github.com/go-ruby-actionpack/actionpack v0.0.0-20261024083700-266f13a8efb0
	github.com/go-ruby-actionview/actionview v0.0.0-20261024083737-676576b44cf7
	github.com/go-ruby-activejob/activejob v0.0.0-20261024083812-9e3ae4347ea9
	github.com/go-ruby-activeldap/activeldap v0.0.0-20261010102626-5ec0ba45ae00
	github.com/go-ruby-activemodel/activemodel v0.0.0-20261024083911-a73143122b94
	github.com/go-ruby-activerecord/activerecord v0.0.0-20261024083945-a69a45062edc
	github.com/go-ruby-activestorage/activestorage v0.0.0-20261024084019-77d56a0fb17c
	github.com/go-ruby-activesupport/activesupport v0.0.0-20261024084053-aff047d906e1
	github.com/go-ruby-addressable/addressable v0.0.0-20261024084127-d18b4a89c303
	github.com/go-ruby-age/age v0.0.0-20261022091113-bfb7050765b4
	github.com/go-ruby-arrow/arrow v0.0.0-20261022091142-83cd2f598c94
	github.com/go-ruby-async/async v0.0.0-20261022091210-d7899ce336a3
	github.com/go-ruby-augeas/augeas v0.0.0-20261011101516-ee3ea9122f51
	github.com/go-ruby-base64/base64 v0.0.0-20261024084341-86195602030f
	github.com/go-ruby-bbolt/bbolt v0.0.0-20261022091327-1c4188110f7b
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20261024084456-755fa818a3e1
	github.com/go-ruby-benchmark/benchmark v0.0.0-20261024084530-aa197564f792
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20261024084624-ff72a537a400
	github.com/go-ruby-bleve/bleve v0.0.0-20261022091518-3f327e4d3ae1
	github.com/go-ruby-builder/builder v0.0.0-20261024084740-0598d1a86716
	github.com/go-ruby-bundler/bundler v0.0.0-20261024084817-cd89ca2886a7
	github.com/go-ruby-cancancan/cancancan v0.0.0-20261024084851-b024ca1b9e96
	github.com/go-ruby-capistrano/capistrano v0.0.0-20261011101527-2b0681a28a45
	github.com/go-ruby-capybara/capybara v0.0.0-20261022091722-6e9cc9cb3b48
	github.com/go-ruby-cgi/cgi v0.0.0-20261024085009-c55d6dceb56a
	github.com/go-ruby-chronic/chronic v0.1.0
	github.com/go-ruby-cmath/cmath v0.0.0-20261024085129-af8da9027053
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20261024085322-24a8c7ac31a1
	github.com/go-ruby-confd/confd v0.0.0-20261011101539-f0a4375869b4
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20261024085422-210b2bfcc901
	github.com/go-ruby-csv/csv v0.0.0-20261024085456-b47068998aee
	github.com/go-ruby-date/date v0.0.0-20261010104352-78655621fab6
	github.com/go-ruby-deep-merge/deep-merge v0.0.0-20261011000614-b5a721bc82ff
	github.com/go-ruby-devise/devise v0.0.0-20261024085657-e3d431a2b6f3
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20261024085732-454059f0c77a
	github.com/go-ruby-digest/digest v0.0.0-20261024085821-cc37f860ff54
	github.com/go-ruby-dotenv/dotenv v0.0.0-20261024085943-9044b64ea846
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20261024090020-dd77e8575e55
	github.com/go-ruby-dry-types/dry-types v0.0.0-20261010104941-c6395db8880d
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20261024090121-ab8f95a66208
	github.com/go-ruby-erb/erb v0.0.0-20261024090224-733d49db38c8
	github.com/go-ruby-erubi/erubi v0.0.0-20261024090258-cd1e7b1f0a36
	github.com/go-ruby-etcd/etcd v0.0.0-20261022092757-d085d802047a
	github.com/go-ruby-excon/excon v0.0.0-20261024090410-19068b7215d8
	github.com/go-ruby-facter/facter v0.0.0-20261011000928-37804f12d4a1
	github.com/go-ruby-factory-bot/factory-bot v0.0.0-20261022092909-a96f3283e7d2
	github.com/go-ruby-faker/faker v0.0.0-20261024090536-d95c05d193a5
	github.com/go-ruby-faraday/faraday v0.0.0-20261024090609-8a318964d956
	github.com/go-ruby-fast-gettext/fast-gettext v0.0.0-20261011001040-802d731d4b5d
	github.com/go-ruby-find/find v0.0.0-20261024090738-f9fa80b89607
	github.com/go-ruby-format/format v0.0.0-20261024090817-6c2187d4883e
	github.com/go-ruby-friendly-id/friendly-id v0.0.0-20261022093157-416b04db75af
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20261024090947-3073ae4bb7f8
	github.com/go-ruby-grape/grape v0.0.0-20261024091026-52602b8a85cb
	github.com/go-ruby-graphql/graphql v0.0.0-20261022093344-ace0f2970b2d
	github.com/go-ruby-grpc/grpc v0.0.0-20261022093416-ef3bf1db5e9a
	github.com/go-ruby-haml/haml v0.0.0-20261010110016-9b77a0ef3d73
	github.com/go-ruby-hanami/hanami v0.0.0-20261024091217-1ec45ed5ebe1
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20261022093537-47d13d45977a
	github.com/go-ruby-hiera/hiera v0.0.0-20261011101656-e154d653546e
	github.com/go-ruby-hocon/hocon v0.0.0-20261011001507-0fa3c310ad3d
	github.com/go-ruby-http/http v0.0.0-20261024091437-fa93fe0cc81b
	github.com/go-ruby-httparty/httparty v0.0.0-20261024091510-080f936b0fc7
	github.com/go-ruby-i18n/i18n v0.0.0-20261024091544-8012599fafed
	github.com/go-ruby-images/images v0.0.0-20261012122107-26cef939ac84
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20261010110509-96f92f7569a2
	github.com/go-ruby-irb/irb v0.0.0-20261024091716-0e479f7e19ca
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20261024091750-ef1d61022628
	github.com/go-ruby-jekyll/jekyll v0.0.0-20261011001543-96a46005b695
	github.com/go-ruby-json/json v0.0.0-20261024091901-6afe1f14e3c9
	github.com/go-ruby-jwt/jwt v0.0.0-20261024091935-827954918317
	github.com/go-ruby-kafka/kafka v0.0.0-20261022094141-269e6f40d6cd
	github.com/go-ruby-kaminari/kaminari v0.0.0-20261022094212-d177850a59ee
	github.com/go-ruby-ldap/ldap v0.0.0-20261022094323-8647afc65fa1
	github.com/go-ruby-logger/logger v0.0.0-20261024092231-67f935a009fb
	github.com/go-ruby-mail/mail v0.0.0-20261024092304-be04c2705c1d
	github.com/go-ruby-marshal/marshal v0.0.0-20261011001655-c4c7450d0365
	github.com/go-ruby-matrix/matrix v0.0.0-20261024092409-9670bd9eee2b
	github.com/go-ruby-mime-types/mime-types v0.0.0-20261024092443-a3c995c124dc
	github.com/go-ruby-minitest/minitest v0.0.0-20261024092521-258f04c48267
	github.com/go-ruby-money/money v0.0.0-20261024092555-43a78ceb5eb6
	github.com/go-ruby-mongodb/mongodb v0.0.0-20261022094724-90aec2334b13
	github.com/go-ruby-msgpack/msgpack v0.0.0-20261024092653-8779dde3a233
	github.com/go-ruby-multi-json/multi-json v0.0.0-20261011101734-b69e6d21e829
	github.com/go-ruby-mysql/mysql v0.0.0-20261010111609-dc15d9505e89
	github.com/go-ruby-nats/nats v0.0.0-20261022094932-926af63cc516
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20261024092915-1b3efde29798
	github.com/go-ruby-net-http/net-http v0.0.0-20261024092954-605ab31588ed
	github.com/go-ruby-net-imap/net-imap v0.0.0-20261010111824-0664f8d0b403
	github.com/go-ruby-net-pop/net-pop v0.0.0-20261024093052-cd804f60de6d
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20261024093153-615e44172aba
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20261010112029-b6ee0a8a032c
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20261024093325-8c3b1a5389e1
	github.com/go-ruby-observer/observer v0.0.0-20261011002010-5d963ebd3b00
	github.com/go-ruby-oidc/oidc v0.0.0-20261022095435-9eef97523aee
	github.com/go-ruby-omniauth/omniauth v0.0.0-20261022095506-a28964eb6ecd
	github.com/go-ruby-openbao/openbao v0.0.0-20261022095534-fbe106e2ba0b
	github.com/go-ruby-openstack/openstack v0.0.0-20261011101755-720270d48827
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20261022095703-2e7155bf180c
	github.com/go-ruby-opentype/opentype v0.4.0
	github.com/go-ruby-optparse/optparse v0.0.0-20261024093810-07a61eda0dc8
	github.com/go-ruby-ostruct/ostruct v0.0.0-20261011101806-33773756bfe5
	github.com/go-ruby-pagy/pagy v0.0.0-20261022095902-0a877327e99b
	github.com/go-ruby-paper-trail/paper-trail v0.0.0-20261022095930-4174f2039e12
	github.com/go-ruby-parquet/parquet v0.0.0-20261022100001-af42b87afc76
	github.com/go-ruby-parser/parser v0.13.1
	github.com/go-ruby-pathname/pathname v0.0.0-20261024094059-4d11ed6d4149
	github.com/go-ruby-pg/pg v0.0.0-20261024094136-f2122e0d1b03
	github.com/go-ruby-prawn/prawn v0.0.0-20261022100143-eea3a3879b55
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20261010113142-58de5cc147ce
	github.com/go-ruby-prime/prime v0.0.0-20261024094314-7bd0cf80dae2
	github.com/go-ruby-protobuf/protobuf v0.0.0-20261022100328-72bc229c9d25
	github.com/go-ruby-pstore/pstore v0.0.0-20261024094421-cd8fb31e22ff
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20261024094458-2ff718165779
	github.com/go-ruby-puma/puma v0.0.0-20261022100448-98122bff3d4b
	github.com/go-ruby-pundit/pundit v0.0.0-20261024094601-0d3a6a15decc
	github.com/go-ruby-puppet-resource-api/puppet-resource-api v0.0.0-20261011002235-d9313f809878
	github.com/go-ruby-puppet/puppet v0.0.0-20261011002311-835b0d33b09a
	github.com/go-ruby-racc/racc v0.0.0-20261024094734-e86e1803b25c
	github.com/go-ruby-rack/rack v0.0.0-20261024094814-1988e76a283a
	github.com/go-ruby-rails/rails v0.0.0-20261022100732-ac5fd468ade5
	github.com/go-ruby-railties/railties v0.0.0-20261024095008-8410957966f8
	github.com/go-ruby-rake/rake v0.0.0-20261024095056-74f529ed51ee
	github.com/go-ruby-ransack/ransack v0.0.0-20261022100850-89c6379ed68b
	github.com/go-ruby-rdoc/rdoc v0.0.0-20261024095235-b665d4ef5ae6
	github.com/go-ruby-redis/redis v0.0.0-20261024095346-c1d456126014
	github.com/go-ruby-regexp/regexp v0.1.1
	github.com/go-ruby-reline/reline v0.0.0-20261024095459-450945773176
	github.com/go-ruby-resolv/resolv v0.0.0-20261010114418-4b7242bf0b64
	github.com/go-ruby-resque/resque v0.0.0-20261022101301-0550a0313b12
	github.com/go-ruby-rexml/rexml v0.0.0-20261024095649-c57ce24cb374
	github.com/go-ruby-roda/roda v0.0.0-20261010114556-0be01a820c16
	github.com/go-ruby-rolify/rolify v0.0.0-20261022101418-dbd7dd65374e
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20261024095913-a6189746fcc8
	github.com/go-ruby-rspec/rspec v0.0.0-20261024095955-1b80d75f4395
	github.com/go-ruby-rss/rss v0.0.0-20261024100038-aede47e0d495
	github.com/go-ruby-rubocop/rubocop v0.0.0-20261013124808-904103bfd760
	github.com/go-ruby-rubygems/rubygems v0.0.0-20261024100206-d2456a2ff10d
	github.com/go-ruby-saml/saml v0.0.0-20261022101732-67f930a3dd00
	github.com/go-ruby-sass/sass v0.0.0-20261004225654-93da1ddfb811
	github.com/go-ruby-scanf/scanf v0.0.0-20261024100349-db1f9cb9c3ac
	github.com/go-ruby-securerandom/securerandom v0.0.0-20261024100440-de6509da644b
	github.com/go-ruby-semantic-puppet/semantic-puppet v0.0.0-20261011002626-2c061b590894
	github.com/go-ruby-sequel/sequel v0.0.0-20261024100553-fc0f8f1483ae
	github.com/go-ruby-shellwords/shellwords v0.0.0-20261024100719-94aa5abf5c69
	github.com/go-ruby-shrine/shrine v0.0.0-20261022102059-d3e6ddecd01b
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20261024100829-27c4e0b97252
	github.com/go-ruby-simplecov/simplecov v0.0.0-20261022102151-955f15ad3c86
	github.com/go-ruby-sinatra/sinatra v0.0.0-20261024100943-21ffc5799dc6
	github.com/go-ruby-slim/slim v0.0.0-20261010115622-9c3a529e6eb2
	github.com/go-ruby-sodium/sodium v0.0.0-20261022102311-1cf91ae811a1
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20261024101129-b7cd281c1e43
	github.com/go-ruby-strscan/strscan v0.0.0-20261024101500-ce784826d96b
	github.com/go-ruby-thor/thor v0.0.0-20261024101622-007c527ee78d
	github.com/go-ruby-timecop/timecop v0.0.0-20261010120219-8d89976e2a27
	github.com/go-ruby-toml/toml v0.0.0-20261024101805-80359e323a29
	github.com/go-ruby-tsort/tsort v0.0.0-20261024101849-e3d580d42e74
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20261024101931-9231c191d5a8
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20261024102013-dd5eb78b2551
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.1.0
	github.com/go-ruby-uri/uri v0.0.0-20261024102139-d01705169a4e
	github.com/go-ruby-vcr/vcr v0.0.0-20261022103024-2910e74d701b
	github.com/go-ruby-warden/warden v0.0.0-20261024102248-b54d8474c67e
	github.com/go-ruby-webauthn/webauthn v0.0.0-20261022103120-a596b0e55977
	github.com/go-ruby-webmock/webmock v0.0.0-20261022103148-6b36bf421db2
	github.com/go-ruby-webrick/webrick v0.0.0-20261024102433-818aed3b0178
	github.com/go-ruby-widgets/mvvm v0.3.0
	github.com/go-ruby-widgets/tui v0.5.0
	github.com/go-ruby-widgets/widgets v0.13.0
	github.com/go-ruby-yaml/yaml v0.1.0
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20261024102812-794ad5ecdbf2
	github.com/go-ruby-zlib/zlib v0.0.0-20261024102853-5e7682d5a079
	github.com/go-statistics/stats v0.1.0
	github.com/go-webauthn/webauthn v0.18.2
	github.com/go-xslt/xslt v0.1.0
	github.com/nats-io/nats-server/v2 v2.15.1
	github.com/redis/go-redis/v9 v9.23.0
	github.com/russellhaering/goxmldsig v1.6.1
	github.com/sirupsen/logrus v1.10.2
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20261022190701-123ec885bc97
	go.etcd.io/etcd/server/v3 v3.7.3
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
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8 // indirect
	github.com/go-augeas/augeas v0.0.0-20260830115849-a0db83a6594a // indirect
	github.com/go-composites/array v0.0.0-20261015235938-f7161fa2d065 // indirect
	github.com/go-composites/error v0.0.0-20261012005711-d4891be7e6e3 // indirect
	github.com/go-composites/null v0.0.0-20261010193318-b5bad10074b1 // indirect
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
	github.com/go-opentype/opentype v0.16.0 // indirect
	github.com/go-opentype/shape v0.7.0 // indirect
	github.com/go-pcore/pcore v0.0.0-20260831114716-f9c3e7f59eaa // indirect
	github.com/go-puppet/puppet v0.0.0-20260918012035-fc6b0424cdbd // indirect
	github.com/go-regexp/engine v0.3.2 // indirect
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
	github.com/klauspost/compress v1.20.3 // indirect
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
	github.com/sergeymakinen/go-bmp v1.0.0 // indirect
	github.com/sergeymakinen/go-ico v1.0.0 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tannevaled/gobig2 v0.2.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/twmb/franz-go v1.22.2 // indirect
	github.com/twmb/franz-go/pkg/kadm v1.19.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.15.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xiang90/probing v0.0.0-20221125231312-a49e3df8f510 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	go.etcd.io/etcd/api/v3 v3.7.3 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.3 // indirect
	go.etcd.io/etcd/client/v3 v3.7.3 // indirect
	go.etcd.io/etcd/pkg/v3 v3.7.3 // indirect
	go.etcd.io/raft/v3 v3.7.0 // indirect
	go.mongodb.org/mongo-driver/v2 v2.9.2 // indirect
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
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20261009151841-97e8ea77aa2a // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.61.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
