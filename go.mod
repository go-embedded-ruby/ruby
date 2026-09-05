module github.com/go-embedded-ruby/ruby

go 1.26.4

require (
	github.com/alicebob/miniredis/v2 v2.39.0
	github.com/beevik/etree v1.7.1
	github.com/dolthub/go-mysql-server v0.20.0
	github.com/glauth/ldap v0.0.0-20260718202943-34c5f9b3cbf1
	github.com/go-commonmark/commonmark v0.1.0
	github.com/go-composites/bag v0.0.0-20260905061328-717cdb9c11b6
	github.com/go-composites/result v0.0.0-20260904101956-f4b09f308e35
	github.com/go-composites/time v0.0.0-20260905061345-a79114038df1
	github.com/go-fft/fft v0.0.0-20260831114610-598cacbd5c9a
	github.com/go-images/images v0.0.0-20260831115433-23d959d868e3
	github.com/go-kramdown/kramdown v0.1.0
	github.com/go-liquid/liquid v0.1.0
	github.com/go-mustache/mustache v0.1.0
	github.com/go-ndarray/ndarray v0.0.0-20260831064201-1c846000bfd5
	github.com/go-nokogiri/nokogiri v0.1.0
	github.com/go-rouge/rouge v0.2.0
	github.com/go-ruby-aasm/aasm v0.0.0-20260717061120-cec0976ec205
	github.com/go-ruby-abbrev/abbrev v0.0.0-20260717061206-761e82f6c6c3
	github.com/go-ruby-acme/acme v0.0.0-20260903192644-92d92d9f688b
	github.com/go-ruby-actioncable/actioncable v0.0.0-20260717061354-49297951bc19
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20260830121614-ca449d6abb1f
	github.com/go-ruby-actionpack/actionpack v0.0.0-20260717061451-b97002255cd7
	github.com/go-ruby-actionview/actionview v0.0.0-20260826125716-4711e2afde65
	github.com/go-ruby-activejob/activejob v0.0.0-20260717061550-62fd193d925a
	github.com/go-ruby-activeldap/activeldap v0.0.0-20260820135513-8f354dd825a4
	github.com/go-ruby-activemodel/activemodel v0.0.0-20260825130957-8a1d518597d9
	github.com/go-ruby-activerecord/activerecord v0.0.0-20260717061644-1c1ede2b8227
	github.com/go-ruby-activestorage/activestorage v0.0.0-20260717061712-16cc7b480fff
	github.com/go-ruby-activesupport/activesupport v0.0.0-20260820071506-344413ecaa5f
	github.com/go-ruby-addressable/addressable v0.0.0-20260717061819-a062ad183c72
	github.com/go-ruby-age/age v0.0.0-20260830121641-3a7438630806
	github.com/go-ruby-arrow/arrow v0.0.0-20260819100858-85314c7b7b47
	github.com/go-ruby-async/async v0.0.0-20260717061939-b24a3ecc37bc
	github.com/go-ruby-augeas/augeas v0.0.0-20260831125504-d45cc71b97b1
	github.com/go-ruby-base64/base64 v0.0.0-20260905061640-0d3d5b32edea
	github.com/go-ruby-bbolt/bbolt v0.0.0-20260717062205-7390a75a22b5
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20260903192730-d0d68e3da0a3
	github.com/go-ruby-benchmark/benchmark v0.0.0-20260717062257-3cc267fbe9b3
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20260717062419-05e9199217e8
	github.com/go-ruby-bleve/bleve v0.0.0-20260825110136-02ab15b86bd5
	github.com/go-ruby-builder/builder v0.0.0-20260717062621-9662d413214e
	github.com/go-ruby-bundler/bundler v0.0.0-20260820220555-bc5ab354ccc0
	github.com/go-ruby-cancancan/cancancan v0.0.0-20260717062720-f61cac26a74b
	github.com/go-ruby-capistrano/capistrano v0.0.0-20260903192711-c466694b6746
	github.com/go-ruby-capybara/capybara v0.0.0-20260820044305-5862fd2c7385
	github.com/go-ruby-cgi/cgi v0.0.0-20260717062833-adf35051de76
	github.com/go-ruby-chronic/chronic v0.0.0-20260717062953-666fcc986ef0
	github.com/go-ruby-cmath/cmath v0.0.0-20260717063021-5f2ebb903ff8
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20260717063333-f81e68518da7
	github.com/go-ruby-confd/confd v0.0.0-20260825131031-1ac780d51c57
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20260717063423-df03c3d25557
	github.com/go-ruby-csv/csv v0.0.0-20260717063450-c0e361e4d3ee
	github.com/go-ruby-date/date v0.0.0-20260717063625-7b8321539439
	github.com/go-ruby-deep-merge/deep-merge v0.0.0-20260825131041-0389f358e6cf
	github.com/go-ruby-devise/devise v0.0.0-20260905061616-05b86cbc31a9
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20260717063932-c1bcb7f2c2eb
	github.com/go-ruby-digest/digest v0.0.0-20260903192710-f831cb18da00
	github.com/go-ruby-dotenv/dotenv v0.0.0-20260717064303-a92f1ce2dea6
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20260820220854-4ffa46cc87bb
	github.com/go-ruby-dry-types/dry-types v0.0.0-20260717064401-2277b3319f9b
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20260820220905-a83ae74d091c
	github.com/go-ruby-erb/erb v0.0.0-20260717064518-1e3bc0812b45
	github.com/go-ruby-erubi/erubi v0.0.0-20260717064544-a50720a20843
	github.com/go-ruby-etcd/etcd v0.0.0-20260826125744-599673ae1904
	github.com/go-ruby-excon/excon v0.0.0-20260717064701-8f7a788d9222
	github.com/go-ruby-facter/facter v0.0.0-20260831125504-d8bb19e1e317
	github.com/go-ruby-factory-bot/factory-bot v0.0.0-20260717064748-e60c0663ca83
	github.com/go-ruby-faker/faker v0.0.0-20260717064814-ee784cec992c
	github.com/go-ruby-faraday/faraday v0.0.0-20260717064841-338275e249af
	github.com/go-ruby-fast-gettext/fast-gettext v0.0.0-20260826125753-7e0d72f9a378
	github.com/go-ruby-find/find v0.0.0-20260717064950-d884f04350dc
	github.com/go-ruby-format/format v0.0.0-20260831115501-f58c7d12507c
	github.com/go-ruby-friendly-id/friendly-id v0.0.0-20260820045709-94331b070f05
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20260717065132-86577a8b648f
	github.com/go-ruby-grape/grape v0.0.0-20260717065202-be187cadbce2
	github.com/go-ruby-graphql/graphql v0.0.0-20260717065229-c0355095acc2
	github.com/go-ruby-grpc/grpc v0.0.0-20260727143307-befa80ff22df
	github.com/go-ruby-haml/haml v0.0.0-20260727125213-455e870e92d2
	github.com/go-ruby-hanami/hanami v0.0.0-20260825131153-04720c8f2b6e
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20260717065417-6b99e6076938
	github.com/go-ruby-hiera/hiera v0.0.0-20260831115655-7a9d33419f3e
	github.com/go-ruby-hocon/hocon v0.0.0-20260901145201-d484ec155199
	github.com/go-ruby-http/http v0.0.0-20260717065554-e85ce36e8298
	github.com/go-ruby-httparty/httparty v0.0.0-20260717065622-fffadbe77679
	github.com/go-ruby-i18n/i18n v0.0.0-20260717065651-df47ab9491f6
	github.com/go-ruby-images/images v0.0.0-20260901145158-9611e9c1df30
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20260717065751-ac3e17f41c3e
	github.com/go-ruby-irb/irb v0.0.0-20260717065819-46d76ed87554
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20260717065847-21755dfd32e9
	github.com/go-ruby-jekyll/jekyll v0.0.0-20260905061623-c06290e6d756
	github.com/go-ruby-json/json v0.0.0-20260803122801-b23aeb96e6ae
	github.com/go-ruby-jwt/jwt v0.0.0-20260717065943-0bba2f39bf81
	github.com/go-ruby-kafka/kafka v0.0.0-20260905061621-6b4f2a9999f3
	github.com/go-ruby-kaminari/kaminari v0.0.0-20260717070041-898c0896ede4
	github.com/go-ruby-ldap/ldap v0.0.0-20260808195309-d90a141d64f9
	github.com/go-ruby-logger/logger v0.0.0-20260717070206-b7582341d8fc
	github.com/go-ruby-mail/mail v0.0.0-20260717070235-fe48e8a63a7c
	github.com/go-ruby-marshal/marshal v0.0.0-20260820215345-e25f276d2451
	github.com/go-ruby-matrix/matrix v0.0.0-20260717070327-18d9569abdf3
	github.com/go-ruby-mime-types/mime-types v0.0.0-20260717070355-9a0ed8093795
	github.com/go-ruby-minitest/minitest v0.0.0-20260717070426-42021a9bc848
	github.com/go-ruby-money/money v0.0.0-20260717070454-fe64c970ee24
	github.com/go-ruby-mongodb/mongodb v0.0.0-20260828162608-25326b5da003
	github.com/go-ruby-msgpack/msgpack v0.0.0-20260803123859-f2d889c3a60f
	github.com/go-ruby-multi-json/multi-json v0.0.0-20260825110324-bdea42ea11b6
	github.com/go-ruby-mysql/mysql v0.0.0-20260903192751-d2c199d0722c
	github.com/go-ruby-nats/nats v0.0.0-20260828162804-c57124b07704
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20260828162846-be29215dbbc7
	github.com/go-ruby-net-http/net-http v0.0.0-20260717070838-ce4150523d5d
	github.com/go-ruby-net-imap/net-imap v0.0.0-20260717070905-3a59f94807c2
	github.com/go-ruby-net-pop/net-pop v0.0.0-20260717070933-31f4fe95b9ef
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20260717071032-1fc64868e092
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20260717071059-96436a2b91b9
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20260717071155-ed422c317bcd
	github.com/go-ruby-observer/observer v0.0.0-20260820220157-5e26c6317a28
	github.com/go-ruby-oidc/oidc v0.0.0-20260825131247-3eec65e55886
	github.com/go-ruby-omniauth/omniauth v0.0.0-20260825131256-b260ffb4a332
	github.com/go-ruby-openbao/openbao v0.0.0-20260717071447-328a091965dd
	github.com/go-ruby-openstack/openstack v0.0.0-20260825110720-3d2b49913552
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20260826125821-3371d170a93c
	github.com/go-ruby-opentype/opentype v0.2.0
	github.com/go-ruby-optparse/optparse v0.0.0-20260717071632-da8194087eaf
	github.com/go-ruby-ostruct/ostruct v0.0.0-20260820220107-4de11f016237
	github.com/go-ruby-pagy/pagy v0.0.0-20260717071719-997d15eee011
	github.com/go-ruby-paper-trail/paper-trail v0.0.0-20260717071745-42a249656e5a
	github.com/go-ruby-parquet/parquet v0.0.0-20260825131318-5f7f0453d409
	github.com/go-ruby-parser/parser v0.1.7
	github.com/go-ruby-pathname/pathname v0.0.0-20260717071958-ef9f4ddd9c32
	github.com/go-ruby-pg/pg v0.0.0-20260717072026-60f97fa236cf
	github.com/go-ruby-prawn/prawn v0.0.0-20260829111617-4a543d91acca
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20260717072124-c98cbe80c502
	github.com/go-ruby-prime/prime v0.0.0-20260717072153-520610c103fc
	github.com/go-ruby-protobuf/protobuf v0.0.0-20260820052513-ddc8d1652a89
	github.com/go-ruby-pstore/pstore v0.0.0-20260825110359-e874d1551968
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20260717072318-6eac22b2dbc2
	github.com/go-ruby-puma/puma v0.0.0-20260717072346-1d0625916636
	github.com/go-ruby-pundit/pundit v0.0.0-20260717072413-19df78d95080
	github.com/go-ruby-puppet-resource-api/puppet-resource-api v0.0.0-20260901145207-1f42f39276b6
	github.com/go-ruby-puppet/puppet v0.0.0-20260901145212-6718ec34c276
	github.com/go-ruby-racc/racc v0.0.0-20260717072524-f2ecff3175b6
	github.com/go-ruby-rack/rack v0.0.0-20260717072552-0eeee2b1ab82
	github.com/go-ruby-rails/rails v0.0.0-20260831132647-16f9e724102e
	github.com/go-ruby-railties/railties v0.0.0-20260825131355-e3f5cf0a22b4
	github.com/go-ruby-rake/rake v0.0.0-20260717072932-0e8cc465a5c7
	github.com/go-ruby-ransack/ransack v0.0.0-20260717072959-06ca1d7c6829
	github.com/go-ruby-rdoc/rdoc v0.0.0-20260717073056-1c2a041dd097
	github.com/go-ruby-redis/redis v0.0.0-20260717073145-6d0931fd170d
	github.com/go-ruby-regexp/regexp v0.0.0-20260831115702-e14375e92d68
	github.com/go-ruby-reline/reline v0.0.0-20260717073242-c4ce28273d52
	github.com/go-ruby-resolv/resolv v0.0.0-20260717073312-9fce615a7c36
	github.com/go-ruby-resque/resque v0.0.0-20260903192756-dc5f8e3f2e80
	github.com/go-ruby-rexml/rexml v0.0.0-20260717073406-283af9b32a91
	github.com/go-ruby-roda/roda v0.0.0-20260825131412-7cf0e280645a
	github.com/go-ruby-rolify/rolify v0.0.0-20260717073459-4d2e717bab13
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20260717073554-6cb1347ad01a
	github.com/go-ruby-rspec/rspec v0.0.0-20260717073621-5d9b4af67459
	github.com/go-ruby-rss/rss v0.0.0-20260820220336-c7b8884919a8
	github.com/go-ruby-rubocop/rubocop v0.0.0-20260831125349-d567951b4130
	github.com/go-ruby-rubygems/rubygems v0.0.0-20260717073743-411e87dbc611
	github.com/go-ruby-saml/saml v0.0.0-20260820151304-ea0a2dc54627
	github.com/go-ruby-sass/sass v0.0.0-20260903192745-4f4b075b94ba
	github.com/go-ruby-scanf/scanf v0.0.0-20260717073842-238d8003a8eb
	github.com/go-ruby-securerandom/securerandom v0.0.0-20260905061628-034ffbd65cbb
	github.com/go-ruby-semantic-puppet/semantic-puppet v0.0.0-20260825110441-e577792d52e8
	github.com/go-ruby-sequel/sequel v0.0.0-20260717074005-d32783333d28
	github.com/go-ruby-shellwords/shellwords v0.0.0-20260717074108-d7e869454d70
	github.com/go-ruby-shrine/shrine v0.0.0-20260717074136-96ee44b6c6c8
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20260903192757-d561fb1ecfb6
	github.com/go-ruby-simplecov/simplecov v0.0.0-20260717074233-00faa55e2495
	github.com/go-ruby-sinatra/sinatra v0.0.0-20260820220259-f411364fd9cb
	github.com/go-ruby-slim/slim v0.0.0-20260727125438-a7b1ba8a9d15
	github.com/go-ruby-sodium/sodium v0.0.0-20260903192737-75c4847f5739
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20260903192751-46639543e2be
	github.com/go-ruby-strscan/strscan v0.0.0-20260901145237-ccc695a615fd
	github.com/go-ruby-thor/thor v0.0.0-20260717074854-30c0ea47941b
	github.com/go-ruby-timecop/timecop v0.0.0-20260717074948-f619efc95b6b
	github.com/go-ruby-toml/toml v0.0.0-20260804183358-41754078f523
	github.com/go-ruby-tsort/tsort v0.0.0-20260717075041-e5642bd3f641
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20260717075109-19c93ad23df7
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20260717075136-fbbfc02c474c
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.0.0-20260824160327-bc72224e43ad
	github.com/go-ruby-uri/uri v0.0.0-20260717075231-3bc88781d7e3
	github.com/go-ruby-vcr/vcr v0.0.0-20260717075257-3b3a803b4619
	github.com/go-ruby-warden/warden v0.0.0-20260825131515-95421a9093e6
	github.com/go-ruby-webauthn/webauthn v0.0.0-20260830160239-83435a4a51e1
	github.com/go-ruby-webmock/webmock v0.0.0-20260717075423-a92c67f51b7f
	github.com/go-ruby-webrick/webrick v0.0.0-20260820220321-f30d6f50d237
	github.com/go-ruby-widgets/mvvm v0.1.0
	github.com/go-ruby-widgets/tui v0.3.0
	github.com/go-ruby-widgets/widgets v0.11.0
	github.com/go-ruby-yaml/yaml v0.0.0-20260804155707-9c1d94ea2290
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20260717075707-df4c9c6e9631
	github.com/go-ruby-zlib/zlib v0.0.0-20260905061644-146cf9612b10
	github.com/go-webauthn/webauthn v0.18.0
	github.com/go-xslt/xslt v0.1.0
	github.com/nats-io/nats-server/v2 v2.14.6
	github.com/redis/go-redis/v9 v9.22.0
	github.com/russellhaering/goxmldsig v1.6.1
	github.com/sirupsen/logrus v1.10.2
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20260905045312-d70fc1b5b9f8
	go.etcd.io/etcd/server/v3 v3.7.1
	golang.org/x/sys v0.47.0
	golang.org/x/text v0.41.0
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
	github.com/ajroetker/go-jpeg2000 v0.0.2 // indirect
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/apache/arrow-go/v18 v18.7.0 // indirect
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
	github.com/fxamacker/cbor/v2 v2.9.3 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8 // indirect
	github.com/go-augeas/augeas v0.0.0-20260830115849-a0db83a6594a // indirect
	github.com/go-composites/array v0.0.0-20260904102020-397f40bbdaca // indirect
	github.com/go-composites/error v0.0.0-20260903220219-cc4a1228280c // indirect
	github.com/go-composites/null v0.0.0-20260903220223-c1d743488d23 // indirect
	github.com/go-crdt/collab v0.25.0 // indirect
	github.com/go-crdt/crdt v0.31.0 // indirect
	github.com/go-datetime/dates v0.1.0 // indirect
	github.com/go-facter/facter v0.0.0-20260830120958-454b72e642ab // indirect
	github.com/go-gfx/gfx v0.19.0 // indirect
	github.com/go-hiera/hiera v0.0.0-20260830144306-f9304f6bec92 // indirect
	github.com/go-hocon/hocon v0.0.0-20260831114632-08e716b40e6d // indirect
	github.com/go-icons/iconoir v0.2.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-ldap/ldap/v3 v3.4.14 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-opentype/fonts v0.8.0 // indirect
	github.com/go-opentype/opentype v0.12.0 // indirect
	github.com/go-opentype/shape v0.5.0 // indirect
	github.com/go-pcore/pcore v0.0.0-20260831114716-f9c3e7f59eaa // indirect
	github.com/go-puppet/puppet v0.0.0-20260831064218-ab6e40079f54 // indirect
	github.com/go-regexp/engine v0.1.3 // indirect
	github.com/go-richdoc/richdoc v0.2.0 // indirect
	github.com/go-ruby-fast-gettext-locale/fast-gettext-locale v0.0.0-20260825110154-a53e0e3a41a7 // indirect
	github.com/go-scss/scss v0.0.0-20260901193330-1ec287aec907 // indirect
	github.com/go-simd/adler32 v0.0.0-20260903215945-099b59e5ad5a // indirect
	github.com/go-simd/base64 v0.0.0-20260903220000-c04f5883bb18 // indirect
	github.com/go-simd/crc32 v0.0.0-20260903220012-5f164e0e0487 // indirect
	github.com/go-simd/hex v0.0.0-20260903220024-a8d22a843218 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/go-typeset/bidi v0.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.0 // indirect
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
	github.com/gophercloud/gophercloud/v2 v2.14.0 // indirect
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
	github.com/nats-io/nats.go v1.53.1 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
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
	github.com/twmb/franz-go v1.21.6 // indirect
	github.com/twmb/franz-go/pkg/kadm v1.18.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xiang90/probing v0.0.0-20221125231312-a49e3df8f510 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	go.etcd.io/etcd/api/v3 v3.7.1 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.1 // indirect
	go.etcd.io/etcd/client/v3 v3.7.1 // indirect
	go.etcd.io/etcd/pkg/v3 v3.7.1 // indirect
	go.etcd.io/raft/v3 v3.7.0 // indirect
	go.mongodb.org/mongo-driver/v2 v2.8.2 // indirect
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
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.58.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
