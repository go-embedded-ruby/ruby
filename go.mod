module github.com/go-embedded-ruby/ruby

go 1.26.4

require (
	github.com/go-composites/bag v0.0.0-20260621180003-a1aa1a8eec62
	github.com/go-composites/result v0.0.0-20260621164801-bc2eac479381
	github.com/go-composites/time v0.0.0-20260620202627-52c1ec9f0af0
	github.com/go-fft/fft v0.0.0-20260620110530-0e3ca1747acb
	github.com/go-images/images v0.0.0-20260620184442-aa6cd1c0beb7
	github.com/go-ndarray/ndarray v0.0.0-20260620170009-555bfc31e7a3
	github.com/go-ruby-abbrev/abbrev v0.0.0-20260629150957-97117892cd38
	github.com/go-ruby-activerecord/activerecord v0.0.0-20260702222646-da57bd9e07f6
	github.com/go-ruby-addressable/addressable v0.0.0-20260701121828-b1a644c57795
	github.com/go-ruby-age/age v0.0.0-20260704110143-130f93385e8a
	github.com/go-ruby-arrow/arrow v0.0.0-20260704111100-7f2676cd9dda
	github.com/go-ruby-base64/base64 v0.0.0-20260703164120-2194be98969e
	github.com/go-ruby-bbolt/bbolt v0.0.0-20260704121138-28ee121195c0
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20260701122042-7e14b6a42363
	github.com/go-ruby-benchmark/benchmark v0.0.0-20260630081339-0d8f1c26e378
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20260703182656-06e4422c5207
	github.com/go-ruby-bleve/bleve v0.0.0-20260704121320-7b342f38e500
	github.com/go-ruby-builder/builder v0.0.0-20260701123755-6ebda00e35ba
	github.com/go-ruby-cgi/cgi v0.0.0-20260629151926-ac1c4d37a56c
	github.com/go-ruby-chronic/chronic v0.0.0-20260702143618-a66a197ca555
	github.com/go-ruby-cmath/cmath v0.0.0-20260629152837-67a84137d824
	github.com/go-ruby-commonmark/commonmark v0.0.0-20260701104528-2d4001975689
	github.com/go-ruby-csv/csv v0.0.0-20260629114549-c624fdf379cc
	github.com/go-ruby-date/date v0.0.0-20260629114559-23a5251a54e4
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20260629152232-d6815db959e9
	github.com/go-ruby-digest/digest v0.0.0-20260703175012-4332315c5957
	github.com/go-ruby-dotenv/dotenv v0.0.0-20260701101948-1022451560fd
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20260702152517-9e25a927a351
	github.com/go-ruby-dry-types/dry-types v0.0.0-20260702150052-94ab5100a720
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20260702151810-a274955bddac
	github.com/go-ruby-erb/erb v0.0.0-20260629074717-0999ae4dd529
	github.com/go-ruby-faker/faker v0.0.0-20260630192057-0a0efdf75352
	github.com/go-ruby-faraday/faraday v0.0.0-20260704105000-d9589491af46
	github.com/go-ruby-find/find v0.0.0-20260630081030-35072d185272
	github.com/go-ruby-format/format v0.0.0-20260703115518-8adcf1b4af5f
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20260629150025-1a1bfd19bc49
	github.com/go-ruby-grape/grape v0.0.0-20260702151528-455377c8c7c3
	github.com/go-ruby-graphql/graphql v0.0.0-20260704114306-47444c09995e
	github.com/go-ruby-grpc/grpc v0.0.0-20260704103831-6f8adb9d540b
	github.com/go-ruby-haml/haml v0.0.0-20260701125233-5bf8084caf1c
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20260630160546-4b7ef3837e5b
	github.com/go-ruby-i18n/i18n v0.0.0-20260630142747-6915e4b870f5
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20260703162306-c9957c9959e1
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20260702144712-895482f62ac3
	github.com/go-ruby-json/json v0.0.0-20260703161943-3c4f2e0302d2
	github.com/go-ruby-jwt/jwt v0.0.0-20260702205900-15884789dfbf
	github.com/go-ruby-kramdown/kramdown v0.0.0-20260630191459-2e9dd5fd0be8
	github.com/go-ruby-liquid/liquid v0.0.0-20260630164624-06905b8b5eaf
	github.com/go-ruby-logger/logger v0.0.0-20260630081511-870e2ee3f277
	github.com/go-ruby-mail/mail v0.0.0-20260701122047-67f8e8ec1d6e
	github.com/go-ruby-marshal/marshal v0.0.0-20260622114304-27ed1baddd9f
	github.com/go-ruby-matrix/matrix v0.0.0-20260630052510-d60a23f08aca
	github.com/go-ruby-mime-types/mime-types v0.0.0-20260630150449-4e5a308a8847
	github.com/go-ruby-money/money v0.0.0-20260702143724-59c9de931e83
	github.com/go-ruby-msgpack/msgpack v0.0.0-20260630150113-002078d2af90
	github.com/go-ruby-mustache/mustache v0.0.0-20260701123847-26d5e451677a
	github.com/go-ruby-nokogiri/nokogiri v0.0.0-20260702164556-6e939959240e
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20260702151234-88fab8d845a1
	github.com/go-ruby-observer/observer v0.0.0-20260630080708-c3a02da51f79
	github.com/go-ruby-oidc/oidc v0.0.0-20260703180815-11ece54216c6
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20260704112350-643c5c130c9c
	github.com/go-ruby-optparse/optparse v0.0.0-20260629093110-6b69a6b03546
	github.com/go-ruby-ostruct/ostruct v0.0.0-20260630080835-69fcd87e76bf
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
	github.com/go-ruby-rack/rack v0.0.0-20260704053028-640136bb67e7
	github.com/go-ruby-redis/redis v0.0.0-20260701125752-5de216f6ad92
	github.com/go-ruby-regexp/regexp v0.0.0-20260703193131-c52ca89ccd08
	github.com/go-ruby-resolv/resolv v0.0.0-20260629153520-df410a5796ac
	github.com/go-ruby-rexml/rexml v0.0.0-20260629154021-5fb0f287ee8b
	github.com/go-ruby-rouge/rouge v0.0.0-20260701044002-71f9c1aaa66c
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20260701142854-896858beadc8
	github.com/go-ruby-rspec/rspec v0.0.0-20260702145830-12badaeb0d75
	github.com/go-ruby-rss/rss v0.0.0-20260630123856-ba95b4fb73c9
	github.com/go-ruby-rubocop/rubocop v0.0.0-20260702170528-0a89da6e9147
	github.com/go-ruby-scanf/scanf v0.0.0-20260629150220-414dbb31c386
	github.com/go-ruby-securerandom/securerandom v0.0.0-20260630081933-3f81ff7d7fb0
	github.com/go-ruby-sequel/sequel v0.0.0-20260702151352-66413b601977
	github.com/go-ruby-set/set v0.0.0-20260703174407-246794df3ec2
	github.com/go-ruby-shellwords/shellwords v0.0.0-20260629114104-e941e4210818
	github.com/go-ruby-sinatra/sinatra v0.0.0-20260630133746-2c894e9d172c
	github.com/go-ruby-slim/slim v0.0.0-20260701141524-ade9ddf6aec4
	github.com/go-ruby-sodium/sodium v0.0.0-20260704110007-85cd070fb270
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20260702143910-c9f337771f41
	github.com/go-ruby-strscan/strscan v0.0.0-20260701044334-d0cc926643a8
	github.com/go-ruby-toml/toml v0.0.0-20260630152206-f9a858ddb785
	github.com/go-ruby-tsort/tsort v0.0.0-20260629151245-27c44f985c8b
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20260701105256-15977bdf6e1a
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.0.0-20260629152419-984d3fbcfb7f
	github.com/go-ruby-uri/uri v0.0.0-20260629113958-59633d1b0deb
	github.com/go-ruby-xslt/xslt v0.0.0-20260702171958-146eaf3f0176
	github.com/go-ruby-yaml/yaml v0.0.0-20260629093916-8035038027bd
	github.com/go-ruby-zlib/zlib v0.0.0-20260704053046-1ff8c43f4f67
)

require (
	filippo.io/age v1.3.1 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.14.5 // indirect
	github.com/apache/arrow-go/v18 v18.6.0 // indirect
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
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-composites/array v0.0.0-20260621062820-1aa11b71d5d6 // indirect
	github.com/go-composites/error v0.0.0-20260621061850-8f949885a586 // indirect
	github.com/go-composites/null v0.0.0-20260621061849-c8074799d5aa // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-pdf/fpdf v0.9.0 // indirect
	github.com/go-simd/adler32 v0.0.0-20260703095822-b2b45fec563b // indirect
	github.com/go-simd/base64 v0.0.0-20260703160615-1d0b2dddc996 // indirect
	github.com/go-simd/crc32 v0.0.0-20260703213456-a1976694a16e // indirect
	github.com/go-simd/hex v0.0.0-20260627054622-d04d429c6aea // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/graphql-go/graphql v0.8.1 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.53.0 // indirect
)
