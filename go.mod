module github.com/go-embedded-ruby/ruby

go 1.26.4

require (
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/beevik/etree v1.6.0
	github.com/dolthub/go-mysql-server v0.20.0
	github.com/go-composites/bag v0.0.0-20260621180003-a1aa1a8eec62
	github.com/go-composites/result v0.0.0-20260621164801-bc2eac479381
	github.com/go-composites/time v0.0.0-20260620202627-52c1ec9f0af0
	github.com/go-fft/fft v0.0.0-20260620110530-0e3ca1747acb
	github.com/go-images/images v0.0.0-20260702213524-ea366b42f216
	github.com/go-ndarray/ndarray v0.0.0-20260620170009-555bfc31e7a3
	github.com/go-ruby-abbrev/abbrev v0.0.0-20260629150957-97117892cd38
	github.com/go-ruby-acme/acme v0.0.0-20260704112859-415fad2a4cbe
	github.com/go-ruby-actioncable/actioncable v0.0.0-20260706115409-19f2e354a783
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20260706115414-f116fa6c4917
	github.com/go-ruby-actionpack/actionpack v0.0.0-20260706115418-8ba13fbf84e5
	github.com/go-ruby-actionview/actionview v0.0.0-20260706115423-0e293eed051a
	github.com/go-ruby-activejob/activejob v0.0.0-20260706172137-830f5bbe94b4
	github.com/go-ruby-activemodel/activemodel v0.0.0-20260706115433-f36d70b64b7a
	github.com/go-ruby-activerecord/activerecord v0.0.0-20260702222646-da57bd9e07f6
	github.com/go-ruby-activestorage/activestorage v0.0.0-20260706115443-60cd3e2689e4
	github.com/go-ruby-activesupport/activesupport v0.0.0-20260706115447-2ae51cb37180
	github.com/go-ruby-addressable/addressable v0.0.0-20260701121828-b1a644c57795
	github.com/go-ruby-age/age v0.0.0-20260704110143-130f93385e8a
	github.com/go-ruby-arrow/arrow v0.0.0-20260704111100-7f2676cd9dda
	github.com/go-ruby-async/async v0.0.0-20260706115507-51a981d1e85d
	github.com/go-ruby-base64/base64 v0.0.0-20260703164120-2194be98969e
	github.com/go-ruby-bbolt/bbolt v0.0.0-20260704121138-28ee121195c0
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20260701122042-7e14b6a42363
	github.com/go-ruby-benchmark/benchmark v0.0.0-20260630081339-0d8f1c26e378
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20260703182656-06e4422c5207
	github.com/go-ruby-bleve/bleve v0.0.0-20260704121320-7b342f38e500
	github.com/go-ruby-builder/builder v0.0.0-20260701123755-6ebda00e35ba
	github.com/go-ruby-bundler/bundler v0.0.0-20260630192314-85e213b45177
	github.com/go-ruby-cancancan/cancancan v0.0.0-20260706115551-06675ef421df
	github.com/go-ruby-capistrano/capistrano v0.0.0-20260707161800-c2d92ee7442d
	github.com/go-ruby-cgi/cgi v0.0.0-20260629151926-ac1c4d37a56c
	github.com/go-ruby-chronic/chronic v0.0.0-20260702143618-a66a197ca555
	github.com/go-ruby-cmath/cmath v0.0.0-20260629152837-67a84137d824
	github.com/go-ruby-commonmark/commonmark v0.0.0-20260701104528-2d4001975689
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20260706115615-cdd5a0c72f9e
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20260706115620-287a916e348b
	github.com/go-ruby-csv/csv v0.0.0-20260629114549-c624fdf379cc
	github.com/go-ruby-date/date v0.0.0-20260629114559-23a5251a54e4
	github.com/go-ruby-devise/devise v0.0.0-20260706115634-252d1cfa97fc
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20260629152232-d6815db959e9
	github.com/go-ruby-digest/digest v0.0.0-20260703175012-4332315c5957
	github.com/go-ruby-dotenv/dotenv v0.0.0-20260701101948-1022451560fd
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20260702152517-9e25a927a351
	github.com/go-ruby-dry-types/dry-types v0.0.0-20260702150052-94ab5100a720
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20260702151810-a274955bddac
	github.com/go-ruby-erb/erb v0.0.0-20260706152133-22f94a380d76
	github.com/go-ruby-erubi/erubi v0.0.0-20260706150033-cfeeadde9120
	github.com/go-ruby-etcd/etcd v0.0.0-20260704112801-4417ab26c89a
	github.com/go-ruby-excon/excon v0.0.0-20260706115718-5ca851d0373f
	github.com/go-ruby-faker/faker v0.0.0-20260630192057-0a0efdf75352
	github.com/go-ruby-faraday/faraday v0.0.0-20260704105000-d9589491af46
	github.com/go-ruby-find/find v0.0.0-20260630081030-35072d185272
	github.com/go-ruby-format/format v0.0.0-20260703115518-8adcf1b4af5f
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20260629150025-1a1bfd19bc49
	github.com/go-ruby-grape/grape v0.0.0-20260702151528-455377c8c7c3
	github.com/go-ruby-graphql/graphql v0.0.0-20260704114306-47444c09995e
	github.com/go-ruby-grpc/grpc v0.0.0-20260705194916-601b52b2269c
	github.com/go-ruby-haml/haml v0.0.0-20260701125233-5bf8084caf1c
	github.com/go-ruby-hanami/hanami v0.0.0-20260706115807-b2af4faf16af
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20260630160546-4b7ef3837e5b
	github.com/go-ruby-http/http v0.0.0-20260706115817-580e321826b3
	github.com/go-ruby-httparty/httparty v0.0.0-20260706115822-e22a29858d82
	github.com/go-ruby-i18n/i18n v0.0.0-20260630142747-6915e4b870f5
	github.com/go-ruby-images/images v0.0.0-20260707161119-23e927d074f8
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20260703162306-c9957c9959e1
	github.com/go-ruby-irb/irb v0.0.0-20260630203552-657e348289b2
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20260702144712-895482f62ac3
	github.com/go-ruby-json/json v0.0.0-20260703161943-3c4f2e0302d2
	github.com/go-ruby-jwt/jwt v0.0.0-20260705184902-40cd404d3c65
	github.com/go-ruby-kafka/kafka v0.0.0-20260704121222-eb98884730d5
	github.com/go-ruby-kramdown/kramdown v0.0.0-20260630191459-2e9dd5fd0be8
	github.com/go-ruby-liquid/liquid v0.0.0-20260630164624-06905b8b5eaf
	github.com/go-ruby-logger/logger v0.0.0-20260630081511-870e2ee3f277
	github.com/go-ruby-mail/mail v0.0.0-20260701122047-67f8e8ec1d6e
	github.com/go-ruby-marshal/marshal v0.0.0-20260622114304-27ed1baddd9f
	github.com/go-ruby-matrix/matrix v0.0.0-20260630052510-d60a23f08aca
	github.com/go-ruby-mime-types/mime-types v0.0.0-20260630150449-4e5a308a8847
	github.com/go-ruby-minitest/minitest v0.0.0-20260630144113-dbcefec3be56
	github.com/go-ruby-money/money v0.0.0-20260702143724-59c9de931e83
	github.com/go-ruby-mongodb/mongodb v0.0.0-20260704115215-792ff280c51b
	github.com/go-ruby-msgpack/msgpack v0.0.0-20260630150113-002078d2af90
	github.com/go-ruby-mustache/mustache v0.0.0-20260701123847-26d5e451677a
	github.com/go-ruby-mysql/mysql v0.0.0-20260704103258-b9ed4a15ba9d
	github.com/go-ruby-nats/nats v0.0.0-20260704105415-42cce800b0e7
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20260630142044-917edc09066e
	github.com/go-ruby-net-http/net-http v0.0.0-20260630124155-c794366ce72f
	github.com/go-ruby-net-imap/net-imap v0.0.0-20260630193116-82cd428e7b0f
	github.com/go-ruby-net-pop/net-pop v0.0.0-20260630142109-66d7036032f5
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20260630142744-9be6f27056d7
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20260630142921-0151ad2e87f5
	github.com/go-ruby-nokogiri/nokogiri v0.0.0-20260702164556-6e939959240e
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20260702151234-88fab8d845a1
	github.com/go-ruby-observer/observer v0.0.0-20260630080708-c3a02da51f79
	github.com/go-ruby-oidc/oidc v0.0.0-20260705185218-08dab6b22572
	github.com/go-ruby-omniauth/omniauth v0.0.0-20260706120026-fb3979b5ef4c
	github.com/go-ruby-openbao/openbao v0.0.0-20260707160751-fcf5670a4d6d
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20260704112350-643c5c130c9c
	github.com/go-ruby-optparse/optparse v0.0.0-20260629093110-6b69a6b03546
	github.com/go-ruby-ostruct/ostruct v0.0.0-20260630080835-69fcd87e76bf
	github.com/go-ruby-parquet/parquet v0.0.0-20260704170648-c7f0507946f7
	github.com/go-ruby-parser/parser v0.0.0-20260703103305-5ae12948602f
	github.com/go-ruby-pathname/pathname v0.0.0-20260629151955-d8d2c4e5f81b
	github.com/go-ruby-pg/pg v0.0.0-20260702135906-e5650264cc5d
	github.com/go-ruby-prawn/prawn v0.0.0-20260704123330-7bae7647bcd1
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20260629152429-60a380e82d7d
	github.com/go-ruby-prime/prime v0.0.0-20260703153932-08f6fe218cd4
	github.com/go-ruby-protobuf/protobuf v0.0.0-20260704100903-2defbe43d396
	github.com/go-ruby-pstore/pstore v0.0.0-20260630081017-0dd55a12f94e
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20260630151503-f308d4002444
	github.com/go-ruby-puma/puma v0.0.0-20260704123157-3b47ea0ad779
	github.com/go-ruby-pundit/pundit v0.0.0-20260706120141-d3cc8a101bc3
	github.com/go-ruby-racc/racc v0.0.0-20260630123809-0d492278523f
	github.com/go-ruby-rack/rack v0.0.0-20260705200150-888027c33329
	github.com/go-ruby-rails/rails v0.0.0-20260706183557-5ddb406695b0
	github.com/go-ruby-railties/railties v0.0.0-20260706120201-300b4fe0f2df
	github.com/go-ruby-rake/rake v0.0.0-20260630123309-28092b465e07
	github.com/go-ruby-ransack/ransack v0.0.0-20260707193940-08f791c256b0
	github.com/go-ruby-rdoc/rdoc v0.0.0-20260702162339-c866323cc54e
	github.com/go-ruby-redis/redis v0.0.0-20260701125752-5de216f6ad92
	github.com/go-ruby-regexp/regexp v0.0.0-20260703193131-c52ca89ccd08
	github.com/go-ruby-reline/reline v0.0.0-20260630130257-c3cc9ab10454
	github.com/go-ruby-resolv/resolv v0.0.0-20260629153520-df410a5796ac
	github.com/go-ruby-resque/resque v0.0.0-20260706120225-d8a1746bdaca
	github.com/go-ruby-rexml/rexml v0.0.0-20260629154021-5fb0f287ee8b
	github.com/go-ruby-roda/roda v0.0.0-20260706120235-cf7106a48eaa
	github.com/go-ruby-rouge/rouge v0.0.0-20260701044002-71f9c1aaa66c
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20260701142854-896858beadc8
	github.com/go-ruby-rspec/rspec v0.0.0-20260702145830-12badaeb0d75
	github.com/go-ruby-rss/rss v0.0.0-20260630123856-ba95b4fb73c9
	github.com/go-ruby-rubocop/rubocop v0.0.0-20260702170528-0a89da6e9147
	github.com/go-ruby-rubygems/rubygems v0.0.0-20260630142147-63db192adc4d
	github.com/go-ruby-saml/saml v0.0.0-20260704115648-11caa3fa0e1f
	github.com/go-ruby-scanf/scanf v0.0.0-20260629150220-414dbb31c386
	github.com/go-ruby-securerandom/securerandom v0.0.0-20260630081933-3f81ff7d7fb0
	github.com/go-ruby-sequel/sequel v0.0.0-20260702151352-66413b601977
	github.com/go-ruby-set/set v0.0.0-20260703174407-246794df3ec2
	github.com/go-ruby-shellwords/shellwords v0.0.0-20260629114104-e941e4210818
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20260706120339-331f956ff069
	github.com/go-ruby-sinatra/sinatra v0.0.0-20260630133746-2c894e9d172c
	github.com/go-ruby-slim/slim v0.0.0-20260701141524-ade9ddf6aec4
	github.com/go-ruby-sodium/sodium v0.0.0-20260704110007-85cd070fb270
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20260702143910-c9f337771f41
	github.com/go-ruby-strscan/strscan v0.0.0-20260701044334-d0cc926643a8
	github.com/go-ruby-thor/thor v0.0.0-20260702145030-faa03e6e0228
	github.com/go-ruby-toml/toml v0.0.0-20260630152206-f9a858ddb785
	github.com/go-ruby-tsort/tsort v0.0.0-20260629151245-27c44f985c8b
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20260706120426-67b7a131561d
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20260701105256-15977bdf6e1a
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.0.0-20260629152419-984d3fbcfb7f
	github.com/go-ruby-uri/uri v0.0.0-20260629113958-59633d1b0deb
	github.com/go-ruby-warden/warden v0.0.0-20260706120446-d887742539ef
	github.com/go-ruby-webauthn/webauthn v0.0.0-20260704120708-35595b0ac27b
	github.com/go-ruby-webrick/webrick v0.0.0-20260630133907-a1380ee7733b
	github.com/go-ruby-xslt/xslt v0.0.0-20260702171958-146eaf3f0176
	github.com/go-ruby-yaml/yaml v0.0.0-20260629093916-8035038027bd
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20260706163820-cda238c0e98c
	github.com/go-ruby-zlib/zlib v0.0.0-20260704053046-1ff8c43f4f67
	github.com/go-webauthn/webauthn v0.17.4
	github.com/nats-io/nats-server/v2 v2.14.3
	github.com/redis/go-redis/v9 v9.21.0
	github.com/russellhaering/goxmldsig v1.6.0
	github.com/sirupsen/logrus v1.9.4
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20260702233442-45013cfc5f26
	go.etcd.io/etcd/server/v3 v3.6.13
)

require (
	filippo.io/age v1.3.1 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.14.5 // indirect
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.0-default-no-op // indirect
	github.com/apache/arrow-go/v18 v18.6.0 // indirect
	github.com/apache/thrift v0.22.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.2 // indirect
	github.com/blevesearch/bleve/v2 v2.6.0 // indirect
	github.com/blevesearch/bleve_index_api v1.3.11 // indirect
	github.com/blevesearch/geo v0.2.5 // indirect
	github.com/blevesearch/go-faiss v1.1.0 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.4.7 // indirect
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
	github.com/blevesearch/zapx/v17 v17.1.2 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/crewjam/saml v0.5.1 // indirect
	github.com/dolthub/flatbuffers/v23 v23.3.3-dh.2 // indirect
	github.com/dolthub/go-icu-regex v0.0.0-20250327004329-6799764f2dad // indirect
	github.com/dolthub/jsonpath v0.0.2-0.20240227200619-19675ab05c71 // indirect
	github.com/dolthub/vitess v0.0.0-20250512224608-8fb9c6ea092c // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/go-composites/array v0.0.0-20260621062820-1aa11b71d5d6 // indirect
	github.com/go-composites/error v0.0.0-20260621061850-8f949885a586 // indirect
	github.com/go-composites/null v0.0.0-20260621061849-c8074799d5aa // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-pdf/fpdf v0.9.0 // indirect
	github.com/go-simd/adler32 v0.0.0-20260703095822-b2b45fec563b // indirect
	github.com/go-simd/base64 v0.0.0-20260703160615-1d0b2dddc996 // indirect
	github.com/go-simd/crc32 v0.0.0-20260703213456-a1976694a16e // indirect
	github.com/go-simd/hex v0.0.0-20260627054622-d04d429c6aea // indirect
	github.com/go-sql-driver/mysql v1.10.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.2.6 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/graphql-go/graphql v0.8.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.0.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/lestrrat-go/strftime v1.0.4 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nats.go v1.52.0 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tetratelabs/wazero v1.8.2 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20201229170055-e5319fda7802 // indirect
	github.com/twmb/franz-go v1.21.5 // indirect
	github.com/twmb/franz-go/pkg/kadm v1.18.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	go.etcd.io/etcd/api/v3 v3.6.13 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.13 // indirect
	go.etcd.io/etcd/client/v3 v3.6.13 // indirect
	go.etcd.io/etcd/pkg/v3 v3.6.13 // indirect
	go.etcd.io/raft/v3 v3.6.0 // indirect
	go.mongodb.org/mongo-driver/v2 v2.7.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.59.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/image v0.43.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/telemetry v0.0.0-20260610154732-fb80ec83bdd9 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.46.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.53.0 // indirect
	sigs.k8s.io/json v0.0.0-20211020170558-c049b76a60c6 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
