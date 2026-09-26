module github.com/go-embedded-ruby/ruby

go 1.26.4

require (
	github.com/alicebob/miniredis/v2 v2.39.0
	github.com/beevik/etree v1.8.1
	github.com/dolthub/go-mysql-server v0.20.0
	github.com/glauth/ldap v0.0.0-20260718202943-34c5f9b3cbf1
	github.com/go-commonmark/commonmark v0.1.0
	github.com/go-composites/bag v0.0.0-20260923203350-0140c174beaf
	github.com/go-composites/result v0.0.0-20260920235032-53e0a08ef62b
	github.com/go-composites/time v0.0.0-20260923000420-0a04dcfd543c
	github.com/go-fft/fft v0.0.0-20260831114610-598cacbd5c9a
	github.com/go-images/images v0.0.0-20260923074905-cdcee44e3c7e
	github.com/go-kramdown/kramdown v0.1.0
	github.com/go-liquid/liquid v0.1.0
	github.com/go-mustache/mustache v0.1.0
	github.com/go-ndarray/ndarray v0.0.0-20260831064201-1c846000bfd5
	github.com/go-nokogiri/nokogiri v0.1.0
	github.com/go-rouge/rouge v0.2.0
	github.com/go-ruby-aasm/aasm v0.0.0-20260717061120-cec0976ec205
	github.com/go-ruby-abbrev/abbrev v0.0.0-20260916090008-ac08b8471830
	github.com/go-ruby-acme/acme v0.0.0-20260910083506-2c4b2786f606
	github.com/go-ruby-actioncable/actioncable v0.0.0-20260916090125-dc336dcdde77
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20260923211205-26d6286e26bf
	github.com/go-ruby-actionpack/actionpack v0.0.0-20260916090236-b987dbf76b25
	github.com/go-ruby-actionview/actionview v0.0.0-20260923211214-d926f1a7e0a5
	github.com/go-ruby-activejob/activejob v0.0.0-20260916090346-271f6f8a2a0f
	github.com/go-ruby-activeldap/activeldap v0.0.0-20260916090418-6bcd3576fdda
	github.com/go-ruby-activemodel/activemodel v0.0.0-20260923211229-79f6029378bf
	github.com/go-ruby-activerecord/activerecord v0.0.0-20260916090523-27a767d9d502
	github.com/go-ruby-activestorage/activestorage v0.0.0-20260916090555-d36e120608b9
	github.com/go-ruby-activesupport/activesupport v0.0.0-20260916090626-10f09966f037
	github.com/go-ruby-addressable/addressable v0.0.0-20260916090658-0f4764ebcf4a
	github.com/go-ruby-age/age v0.0.0-20260830121641-3a7438630806
	github.com/go-ruby-arrow/arrow v0.0.0-20260910084041-fd3a53122222
	github.com/go-ruby-async/async v0.0.0-20260717061939-b24a3ecc37bc
	github.com/go-ruby-augeas/augeas v0.0.0-20260831125504-d45cc71b97b1
	github.com/go-ruby-base64/base64 v0.0.0-20260916090912-e1e211a81fd1
	github.com/go-ruby-bbolt/bbolt v0.0.0-20260717062205-7390a75a22b5
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20260916091024-e5b4ef398eb3
	github.com/go-ruby-benchmark/benchmark v0.0.0-20260916091057-fa497ecfe30c
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20260916091144-b31c0a1caac2
	github.com/go-ruby-bleve/bleve v0.0.0-20260825110136-02ab15b86bd5
	github.com/go-ruby-builder/builder v0.0.0-20260916091257-7850ebb53ff7
	github.com/go-ruby-bundler/bundler v0.0.0-20260923211304-8585fc1ac0ac
	github.com/go-ruby-cancancan/cancancan v0.0.0-20260916091405-8fac9e430552
	github.com/go-ruby-capistrano/capistrano v0.0.0-20260903192711-c466694b6746
	github.com/go-ruby-capybara/capybara v0.0.0-20260910084630-0731c9ea8d6d
	github.com/go-ruby-cgi/cgi v0.0.0-20260916091525-3d328d3ff447
	github.com/go-ruby-chronic/chronic v0.0.0-20260916091614-bd47d560b6f4
	github.com/go-ruby-cmath/cmath v0.0.0-20260916091648-4963ab4145f7
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20260916091840-b820f7a57284
	github.com/go-ruby-confd/confd v0.0.0-20260825131031-1ac780d51c57
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20260916091940-5582ed60f8a6
	github.com/go-ruby-csv/csv v0.0.0-20260916092014-f49077ac37dd
	github.com/go-ruby-date/date v0.0.0-20260916092103-cf7111f88dea
	github.com/go-ruby-deep-merge/deep-merge v0.0.0-20260825131041-0389f358e6cf
	github.com/go-ruby-devise/devise v0.0.0-20260923211402-b658b97dbfef
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20260916092307-ddd074d76c3c
	github.com/go-ruby-digest/digest v0.0.0-20260916092355-76988b665a30
	github.com/go-ruby-dotenv/dotenv v0.0.0-20260916092517-652b01333e00
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20260923203755-f9de2d043c8d
	github.com/go-ruby-dry-types/dry-types v0.0.0-20260917093022-4ace620129fe
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20260923203804-e8968bd489d4
	github.com/go-ruby-erb/erb v0.0.0-20260916092736-2b6ca70e94b1
	github.com/go-ruby-erubi/erubi v0.0.0-20260916092808-e08929a3f546
	github.com/go-ruby-etcd/etcd v0.0.0-20260923210448-a14e302251b1
	github.com/go-ruby-excon/excon v0.0.0-20260916092904-2d7d3ea939bd
	github.com/go-ruby-facter/facter v0.0.0-20260831125504-d8bb19e1e317
	github.com/go-ruby-factory-bot/factory-bot v0.0.0-20260717064748-e60c0663ca83
	github.com/go-ruby-faker/faker v0.0.0-20260916093030-7f41896b308e
	github.com/go-ruby-faraday/faraday v0.0.0-20260916093104-ea49608c0ed0
	github.com/go-ruby-fast-gettext/fast-gettext v0.0.0-20260826125753-7e0d72f9a378
	github.com/go-ruby-find/find v0.0.0-20260916093229-03f8985707b5
	github.com/go-ruby-format/format v0.0.0-20260916093306-887300990e07
	github.com/go-ruby-friendly-id/friendly-id v0.0.0-20260910090127-4e0134da5c10
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20260916093430-ea78ae8c529b
	github.com/go-ruby-grape/grape v0.0.0-20260916093510-22ee43652ee8
	github.com/go-ruby-graphql/graphql v0.0.0-20260717065229-c0355095acc2
	github.com/go-ruby-grpc/grpc v0.0.0-20260727143307-befa80ff22df
	github.com/go-ruby-haml/haml v0.0.0-20260916093627-8783fea9fe40
	github.com/go-ruby-hanami/hanami v0.0.0-20260923211533-24f30a036b61
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20260717065417-6b99e6076938
	github.com/go-ruby-hiera/hiera v0.0.0-20260831115655-7a9d33419f3e
	github.com/go-ruby-hocon/hocon v0.0.0-20260901145201-d484ec155199
	github.com/go-ruby-http/http v0.0.0-20260916093911-fc825633cf27
	github.com/go-ruby-httparty/httparty v0.0.0-20260916093943-2584adf002b1
	github.com/go-ruby-i18n/i18n v0.0.0-20260916094015-15d33383fb6e
	github.com/go-ruby-images/images v0.0.0-20260923211542-bdb183a4050c
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20260916094116-8c16b4082722
	github.com/go-ruby-irb/irb v0.0.0-20260916094148-b11dd34a8d63
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20260916094222-db456ca7e642
	github.com/go-ruby-jekyll/jekyll v0.0.0-20260907185853-82fc8b8e7b41
	github.com/go-ruby-json/json v0.0.0-20260916094324-3ddd0f57b467
	github.com/go-ruby-jwt/jwt v0.0.0-20260717065943-0bba2f39bf81
	github.com/go-ruby-kafka/kafka v0.0.0-20260923211608-11947663b174
	github.com/go-ruby-kaminari/kaminari v0.0.0-20260717070041-898c0896ede4
	github.com/go-ruby-ldap/ldap v0.0.0-20260808195309-d90a141d64f9
	github.com/go-ruby-logger/logger v0.0.0-20260916094709-0005da481f56
	github.com/go-ruby-mail/mail v0.0.0-20260916094742-929003e330ea
	github.com/go-ruby-marshal/marshal v0.0.0-20260820215345-e25f276d2451
	github.com/go-ruby-matrix/matrix v0.0.0-20260916094843-418993c373b7
	github.com/go-ruby-mime-types/mime-types v0.0.0-20260916094916-b81f94313451
	github.com/go-ruby-minitest/minitest v0.0.0-20260916094951-890fe6bdb67a
	github.com/go-ruby-money/money v0.0.0-20260916095024-7c6146302d19
	github.com/go-ruby-mongodb/mongodb v0.0.0-20260912084905-df8743006be1
	github.com/go-ruby-msgpack/msgpack v0.0.0-20260916095121-f81da43702e1
	github.com/go-ruby-multi-json/multi-json v0.0.0-20260825110324-bdea42ea11b6
	github.com/go-ruby-mysql/mysql v0.0.0-20260903192751-d2c199d0722c
	github.com/go-ruby-nats/nats v0.0.0-20260920095939-743be351c042
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20260916095353-e39a60e507d2
	github.com/go-ruby-net-http/net-http v0.0.0-20260916095427-514f82c0c217
	github.com/go-ruby-net-imap/net-imap v0.0.0-20260916095458-5766481d6882
	github.com/go-ruby-net-pop/net-pop v0.0.0-20260916095531-fc392a73c587
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20260916095628-7e65f73bbebc
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20260916095659-4ce00c942d53
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20260916095803-9bbe291c2c47
	github.com/go-ruby-observer/observer v0.0.0-20260820220157-5e26c6317a28
	github.com/go-ruby-oidc/oidc v0.0.0-20260923211633-65f2c6985066
	github.com/go-ruby-omniauth/omniauth v0.0.0-20260923203916-25cee0673a57
	github.com/go-ruby-openbao/openbao v0.0.0-20260717071447-328a091965dd
	github.com/go-ruby-openstack/openstack v0.0.0-20260923203924-9000c374d63c
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20260826125821-3371d170a93c
	github.com/go-ruby-opentype/opentype v0.2.0
	github.com/go-ruby-optparse/optparse v0.0.0-20260917100925-33b18da76c37
	github.com/go-ruby-ostruct/ostruct v0.0.0-20260820220107-4de11f016237
	github.com/go-ruby-pagy/pagy v0.0.0-20260717071719-997d15eee011
	github.com/go-ruby-paper-trail/paper-trail v0.0.0-20260717071745-42a249656e5a
	github.com/go-ruby-parquet/parquet v0.0.0-20260923211642-bc75bcb9031e
	github.com/go-ruby-parser/parser v0.5.0
	github.com/go-ruby-pathname/pathname v0.0.0-20260916100446-0824484b665f
	github.com/go-ruby-pg/pg v0.0.0-20260916100523-3831ec811a67
	github.com/go-ruby-prawn/prawn v0.0.0-20260829111617-4a543d91acca
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20260916100624-a502f95c3ad3
	github.com/go-ruby-prime/prime v0.0.0-20260916100658-bce9183a6c57
	github.com/go-ruby-protobuf/protobuf v0.0.0-20260820052513-ddc8d1652a89
	github.com/go-ruby-pstore/pstore v0.0.0-20260916100755-22600eaa8ef5
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20260916100829-97fcf5fc0909
	github.com/go-ruby-puma/puma v0.0.0-20260717072346-1d0625916636
	github.com/go-ruby-pundit/pundit v0.0.0-20260916100927-31bbe9228061
	github.com/go-ruby-puppet-resource-api/puppet-resource-api v0.0.0-20260901145207-1f42f39276b6
	github.com/go-ruby-puppet/puppet v0.0.0-20260923211650-6955c245474b
	github.com/go-ruby-racc/racc v0.0.0-20260916101058-25c21ceace36
	github.com/go-ruby-rack/rack v0.0.0-20260916101131-d86b924dd331
	github.com/go-ruby-rails/rails v0.0.0-20260923204112-21b35dca661b
	github.com/go-ruby-railties/railties v0.0.0-20260916101326-3a999a032a06
	github.com/go-ruby-rake/rake v0.0.0-20260916101401-3a87d07573fb
	github.com/go-ruby-ransack/ransack v0.0.0-20260717072959-06ca1d7c6829
	github.com/go-ruby-rdoc/rdoc v0.0.0-20260916101528-7ed1d61f5088
	github.com/go-ruby-redis/redis v0.0.0-20260916101628-3b91d134e9dc
	github.com/go-ruby-regexp/regexp v0.0.0-20260831115702-e14375e92d68
	github.com/go-ruby-reline/reline v0.0.0-20260916101734-37c7d2c11d74
	github.com/go-ruby-resolv/resolv v0.0.0-20260916101809-a2b1b027ae6d
	github.com/go-ruby-resque/resque v0.0.0-20260903192756-dc5f8e3f2e80
	github.com/go-ruby-rexml/rexml v0.0.0-20260916101905-c8e846bf8b6e
	github.com/go-ruby-roda/roda v0.0.0-20260923204143-337fa1a774a7
	github.com/go-ruby-rolify/rolify v0.0.0-20260717073459-4d2e717bab13
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20260916102108-c0062622e02f
	github.com/go-ruby-rspec/rspec v0.0.0-20260916102141-613d88c1871f
	github.com/go-ruby-rss/rss v0.0.0-20260923204152-16e2242a2336
	github.com/go-ruby-rubocop/rubocop v0.0.0-20260907185859-f69aff2b309d
	github.com/go-ruby-rubygems/rubygems v0.0.0-20260916102314-9bd0bf6a00e5
	github.com/go-ruby-saml/saml v0.0.0-20260925100912-118142122e06
	github.com/go-ruby-sass/sass v0.0.0-20260906100410-777830f19847
	github.com/go-ruby-scanf/scanf v0.0.0-20260916102437-76341a9284aa
	github.com/go-ruby-securerandom/securerandom v0.0.0-20260916102518-8f55919c115b
	github.com/go-ruby-semantic-puppet/semantic-puppet v0.0.0-20260825110441-e577792d52e8
	github.com/go-ruby-sequel/sequel v0.0.0-20260916102612-ac9710f8a470
	github.com/go-ruby-shellwords/shellwords v0.0.0-20260916102720-dcaddc4ec112
	github.com/go-ruby-shrine/shrine v0.0.0-20260717074136-96ee44b6c6c8
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20260916102813-4c8927cd45b5
	github.com/go-ruby-simplecov/simplecov v0.0.0-20260717074233-00faa55e2495
	github.com/go-ruby-sinatra/sinatra v0.0.0-20260923204248-301a06b96ca0
	github.com/go-ruby-slim/slim v0.0.0-20260916102940-76e23809c696
	github.com/go-ruby-sodium/sodium v0.0.0-20260910094952-712708ed6e8d
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20260917103921-b9e613e0720e
	github.com/go-ruby-strscan/strscan v0.0.0-20260916103346-94924cbd2f12
	github.com/go-ruby-thor/thor v0.0.0-20260916103450-ad4daa2c5591
	github.com/go-ruby-timecop/timecop v0.0.0-20260717074948-f619efc95b6b
	github.com/go-ruby-toml/toml v0.0.0-20260916103606-531bde8e061c
	github.com/go-ruby-tsort/tsort v0.0.0-20260916103639-a7d9f97ff914
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20260916103712-7a85bb37a39c
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20260916103743-89200c78075f
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.0.0-20260916103816-ff4e6b030f1f
	github.com/go-ruby-uri/uri v0.0.0-20260916103846-a8de1e4e5937
	github.com/go-ruby-vcr/vcr v0.0.0-20260717075257-3b3a803b4619
	github.com/go-ruby-warden/warden v0.0.0-20260916103943-d05993fea025
	github.com/go-ruby-webauthn/webauthn v0.0.0-20260921104542-2bb711883889
	github.com/go-ruby-webmock/webmock v0.0.0-20260717075423-a92c67f51b7f
	github.com/go-ruby-webrick/webrick v0.0.0-20260917104943-776f07308db4
	github.com/go-ruby-widgets/mvvm v0.1.0
	github.com/go-ruby-widgets/tui v0.3.0
	github.com/go-ruby-widgets/widgets v0.11.0
	github.com/go-ruby-yaml/yaml v0.0.0-20260916104302-910ced2db1c7
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20260916104335-29169305e951
	github.com/go-ruby-zlib/zlib v0.0.0-20260916104412-cebc266933cb
	github.com/go-webauthn/webauthn v0.18.2
	github.com/go-xslt/xslt v0.1.0
	github.com/nats-io/nats-server/v2 v2.15.0
	github.com/redis/go-redis/v9 v9.22.0
	github.com/russellhaering/goxmldsig v1.6.1
	github.com/sirupsen/logrus v1.10.2
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20260925040417-67711bad7b74
	go.etcd.io/etcd/server/v3 v3.7.2
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
)

require (
	filippo.io/age v1.3.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.14.5 // indirect
	github.com/abtreece/confd v0.41.2 // indirect
	github.com/ajroetker/go-highway v0.0.4 // indirect
	github.com/andybalholm/brotli v1.2.3 // indirect
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
	github.com/go-composites/array v0.0.0-20260922235702-4fc43dd1da2c // indirect
	github.com/go-composites/error v0.0.0-20260918235114-2990a9d33571 // indirect
	github.com/go-composites/null v0.0.0-20260903220223-c1d743488d23 // indirect
	github.com/go-crdt/collab v0.25.0 // indirect
	github.com/go-crdt/crdt v0.31.0 // indirect
	github.com/go-datetime/dates v0.1.0 // indirect
	github.com/go-encryptions/unixcrypt v0.1.0 // indirect
	github.com/go-facter/facter v0.0.0-20260830120958-454b72e642ab // indirect
	github.com/go-gfx/gfx v0.26.0 // indirect
	github.com/go-hiera/hiera v0.0.0-20260830144306-f9304f6bec92 // indirect
	github.com/go-hocon/hocon v0.0.0-20260831114632-08e716b40e6d // indirect
	github.com/go-icons/iconoir v0.2.0 // indirect
	github.com/go-images/jpeg2000 v0.1.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-ldap/ldap/v3 v3.4.14 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-opentype/fonts v0.8.0 // indirect
	github.com/go-opentype/opentype v0.12.0 // indirect
	github.com/go-opentype/shape v0.5.0 // indirect
	github.com/go-pcore/pcore v0.0.0-20260831114716-f9c3e7f59eaa // indirect
	github.com/go-puppet/puppet v0.0.0-20260918012035-fc6b0424cdbd // indirect
	github.com/go-regexp/engine v0.1.3 // indirect
	github.com/go-richdoc/richdoc v0.2.0 // indirect
	github.com/go-ruby-fast-gettext-locale/fast-gettext-locale v0.0.0-20260825110154-a53e0e3a41a7 // indirect
	github.com/go-scss/scss v0.0.0-20260905061546-39932e01faa4 // indirect
	github.com/go-simd/adler32 v0.0.0-20260903215945-099b59e5ad5a // indirect
	github.com/go-simd/base64 v0.0.0-20260903220000-c04f5883bb18 // indirect
	github.com/go-simd/crc32 v0.0.0-20260903220012-5f164e0e0487 // indirect
	github.com/go-simd/hex v0.0.0-20260903220024-a8d22a843218 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/go-typeset/bidi v0.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/go-widgets/mvvm v0.8.0 // indirect
	github.com/go-widgets/painter v0.11.0 // indirect
	github.com/go-widgets/toolkit v0.288.0 // indirect
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
	github.com/klauspost/compress v1.20.0 // indirect
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
	github.com/tannevaled/gobig2 v0.1.0 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/twmb/franz-go v1.22.0 // indirect
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
	go.mongodb.org/mongo-driver/v2 v2.9.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.59.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
