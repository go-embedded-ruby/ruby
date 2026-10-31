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
	github.com/go-fft/fft v0.28.6
	github.com/go-images/images v0.1.0
	github.com/go-kramdown/kramdown v0.3.1
	github.com/go-liquid/liquid v0.2.0
	github.com/go-mustache/mustache v0.1.0
	github.com/go-ndarray/ndarray v0.28.0
	github.com/go-nokogiri/nokogiri v0.1.0
	github.com/go-numerics/integrate v0.2.0
	github.com/go-numerics/optimize v0.3.1
	github.com/go-numerics/special v0.2.0
	github.com/go-rouge/rouge v0.7.1
	github.com/go-ruby-aasm/aasm v0.0.0-20261025185733-ae3deccc721d
	github.com/go-ruby-abbrev/abbrev v0.0.0-20261025185738-0c54549fd16a
	github.com/go-ruby-acme/acme v0.0.0-20261025190735-e5aebae8127c
	github.com/go-ruby-actioncable/actioncable v0.0.0-20261025190740-a51691dc6f90
	github.com/go-ruby-actionmailer/actionmailer v0.0.0-20261025190746-803dc588e8b9
	github.com/go-ruby-actionpack/actionpack v0.0.0-20261025190751-e84aa5a931ca
	github.com/go-ruby-actionview/actionview v0.0.0-20261029104351-a1affe629c18
	github.com/go-ruby-activejob/activejob v0.0.0-20261025190801-784f7d9da7cb
	github.com/go-ruby-activeldap/activeldap v0.0.0-20261027093605-c79e446abcf4
	github.com/go-ruby-activemodel/activemodel v0.0.0-20261025180810-3eef0d2cbe84
	github.com/go-ruby-activerecord/activerecord v0.0.0-20261025190812-869d2bcf9f0e
	github.com/go-ruby-activestorage/activestorage v0.0.0-20261025190817-cb839bd9493f
	github.com/go-ruby-activesupport/activesupport v0.0.0-20261025190822-0b02c3632793
	github.com/go-ruby-addressable/addressable v0.0.0-20261025190827-5ccc6e104d9b
	github.com/go-ruby-age/age v0.0.0-20261025190832-e610be9ea7de
	github.com/go-ruby-arrow/arrow v0.0.0-20261025190837-78c97af36367
	github.com/go-ruby-async/async v0.0.0-20261025190841-88c9d8d65c92
	github.com/go-ruby-augeas/augeas v0.0.0-20261025190846-1ef440fea116
	github.com/go-ruby-base64/base64 v0.0.0-20261025190851-23e44851e0d0
	github.com/go-ruby-bbolt/bbolt v0.0.0-20261025180814-df57fdd82539
	github.com/go-ruby-bcrypt/bcrypt v0.0.0-20261025190856-ae39ef6ae083
	github.com/go-ruby-benchmark/benchmark v0.0.0-20261025190901-83dc0402fd18
	github.com/go-ruby-bigdecimal/bigdecimal v0.0.0-20261025190906-3d92de0edde9
	github.com/go-ruby-bleve/bleve v0.0.0-20261025190911-357e55f92c87
	github.com/go-ruby-builder/builder v0.0.0-20261025190916-55765b60c764
	github.com/go-ruby-bundler/bundler v0.0.0-20261025190921-5d19e5a62a84
	github.com/go-ruby-cancancan/cancancan v0.0.0-20261025190926-82934b0c5ed1
	github.com/go-ruby-capistrano/capistrano v0.0.0-20261025190931-ac9a97d0ffb4
	github.com/go-ruby-capybara/capybara v0.0.0-20261029105942-62d37b9e3d21
	github.com/go-ruby-cgi/cgi v0.0.0-20261025190936-de40d41be768
	github.com/go-ruby-chronic/chronic v0.1.0
	github.com/go-ruby-cmath/cmath v0.0.0-20261025190946-da6df1cc25a0
	github.com/go-ruby-concurrent-ruby/concurrent-ruby v0.0.0-20261025191002-0d1278371c0a
	github.com/go-ruby-confd/confd v0.0.0-20261025191007-da1a1d819739
	github.com/go-ruby-connection-pool/connection-pool v0.0.0-20261025191012-ccaca9f2867c
	github.com/go-ruby-csv/csv v0.0.0-20261025191018-cea6eb511a21
	github.com/go-ruby-date/date v0.0.0-20261027095220-466c6063bf6f
	github.com/go-ruby-deep-merge/deep-merge v0.0.0-20261025191028-c57fe594af17
	github.com/go-ruby-devise/devise v0.0.0-20261025191034-3bd524642d71
	github.com/go-ruby-did-you-mean/did-you-mean v0.0.0-20261025191039-fbee64e5aac2
	github.com/go-ruby-digest/digest v0.0.0-20261025191044-9abd0196bf5e
	github.com/go-ruby-dotenv/dotenv v0.0.0-20261025191054-fff6ed263443
	github.com/go-ruby-dry-struct/dry-struct v0.0.0-20261025191059-e7ee508713e8
	github.com/go-ruby-dry-types/dry-types v0.0.0-20261027095726-8e9da7388b6d
	github.com/go-ruby-dry-validation/dry-validation v0.0.0-20261025191110-2d17dadcba95
	github.com/go-ruby-erb/erb v0.0.0-20261025191115-afc28c2523b6
	github.com/go-ruby-erubi/erubi v0.0.0-20261025191120-94500ca9c74d
	github.com/go-ruby-etcd/etcd v0.0.0-20261030111344-0ec4466724b2
	github.com/go-ruby-excon/excon v0.0.0-20261025191125-5e77a6393a3b
	github.com/go-ruby-facter/facter v0.0.0-20261025191130-219cd85a8a9f
	github.com/go-ruby-factory-bot/factory-bot v0.0.0-20261025191136-f47c489e6df3
	github.com/go-ruby-faker/faker v0.0.0-20261025191142-fdfe2c7f9fe5
	github.com/go-ruby-faraday/faraday v0.0.0-20261025191147-a5391f339986
	github.com/go-ruby-fast-gettext/fast-gettext v0.0.0-20261025191158-5c1e7051f686
	github.com/go-ruby-find/find v0.0.0-20261025191203-0bb87377e68b
	github.com/go-ruby-format/format v0.0.0-20261025191209-de3217541ceb
	github.com/go-ruby-friendly-id/friendly-id v0.0.0-20261025191214-31bbb689232c
	github.com/go-ruby-getoptlong/getoptlong v0.0.0-20261025191219-c94fdd74d15e
	github.com/go-ruby-grape/grape v0.0.0-20261025191224-7c48565065cf
	github.com/go-ruby-graphql/graphql v0.0.0-20261025191229-886a1260f2da
	github.com/go-ruby-grpc/grpc v0.0.0-20261028105630-c5ef53de4c9d
	github.com/go-ruby-haml/haml v0.0.0-20261027101024-6fa791a4e4c5
	github.com/go-ruby-hanami/hanami v0.0.0-20261025191247-c2f4e48303be
	github.com/go-ruby-hcl2/hcl2 v0.0.0-20261025191252-70f86f39a825
	github.com/go-ruby-hiera/hiera v0.0.0-20261025191303-23a924f56a7a
	github.com/go-ruby-hocon/hocon v0.0.0-20261025191308-30e96d61aedb
	github.com/go-ruby-http/http v0.0.0-20261025191313-b9d7ce1b05cb
	github.com/go-ruby-httparty/httparty v0.0.0-20261025191318-58806b65b721
	github.com/go-ruby-i18n/i18n v0.0.0-20261025191323-8537ead1ee07
	github.com/go-ruby-images/images v0.0.0-20261027101535-4c10dd387466
	github.com/go-ruby-ipaddr/ipaddr v0.0.0-20261027101623-32aa71f1e606
	github.com/go-ruby-irb/irb v0.1.0
	github.com/go-ruby-jbuilder/jbuilder v0.0.0-20261025191345-b010faff6476
	github.com/go-ruby-jekyll/jekyll v0.0.0-20261025191350-733aaef14f2f
	github.com/go-ruby-json/json v0.0.0-20261025191355-09eb07b50ade
	github.com/go-ruby-jwt/jwt v0.0.0-20261025191401-92a0d61dd630
	github.com/go-ruby-kafka/kafka v0.0.0-20261025191406-d8029946e3ed
	github.com/go-ruby-kaminari/kaminari v0.0.0-20261025191411-3ff3153e31f7
	github.com/go-ruby-ldap/ldap v0.0.0-20261025191422-92210e51612c
	github.com/go-ruby-logger/logger v0.0.0-20261025191432-78187c675a89
	github.com/go-ruby-mail/mail v0.0.0-20261025191438-f073d7478a9b
	github.com/go-ruby-marshal/marshal v0.0.0-20261011001655-c4c7450d0365
	github.com/go-ruby-matrix/matrix v0.0.0-20261025191443-9cf274fa5d1e
	github.com/go-ruby-mime-types/mime-types v0.0.0-20261025191448-8175684180d8
	github.com/go-ruby-minitest/minitest v0.0.0-20261025191453-6d19c2f510f9
	github.com/go-ruby-money/money v0.0.0-20261025191458-08a8811bed69
	github.com/go-ruby-mongodb/mongodb v0.0.0-20261025191504-a448bee962f9
	github.com/go-ruby-msgpack/msgpack v0.0.0-20261025191509-325db085b22b
	github.com/go-ruby-multi-json/multi-json v0.0.0-20261025191515-ad726175dda0
	github.com/go-ruby-mysql/mysql v0.0.0-20261027102859-c43cdbbd8b45
	github.com/go-ruby-nats/nats v0.0.0-20261028111811-3fe3a0ef6634
	github.com/go-ruby-net-ftp/net-ftp v0.0.0-20261025191536-10ca85c901be
	github.com/go-ruby-net-http/net-http v0.0.0-20261025191541-09f4b041af7a
	github.com/go-ruby-net-imap/net-imap v0.0.0-20261027103141-afac0b25e02e
	github.com/go-ruby-net-pop/net-pop v0.0.0-20261025191551-b390bcdd970b
	github.com/go-ruby-net-sftp/net-sftp v0.0.0-20261025191602-89f2713d6a79
	github.com/go-ruby-net-smtp/net-smtp v0.0.0-20261027103402-825004ebbef7
	github.com/go-ruby-oauth2/oauth2 v0.0.0-20261025191618-1b344903b3b9
	github.com/go-ruby-observer/observer v0.0.0-20261011002010-5d963ebd3b00
	github.com/go-ruby-oidc/oidc v0.0.0-20261025191625-8814f6d00d43
	github.com/go-ruby-omniauth/omniauth v0.0.0-20261025191630-230594aa2bfa
	github.com/go-ruby-openbao/openbao v0.0.0-20261025191635-033b89746782
	github.com/go-ruby-openstack/openstack v0.0.0-20261025191645-af3a4931865d
	github.com/go-ruby-opentelemetry/opentelemetry v0.0.0-20261025220006-1f8e731805de
	github.com/go-ruby-opentype/opentype v0.4.0
	github.com/go-ruby-optparse/optparse v0.0.0-20261025191650-6ad8c75c6cfc
	github.com/go-ruby-ostruct/ostruct v0.0.0-20261025191656-7a079feab9e3
	github.com/go-ruby-pagy/pagy v0.0.0-20261025191701-053fcd4aa5cc
	github.com/go-ruby-paper-trail/paper-trail v0.0.0-20261025191706-ca25fcaea25a
	github.com/go-ruby-parquet/parquet v0.0.0-20261025191711-6e7a95f7a303
	github.com/go-ruby-parser/parser v0.13.1
	github.com/go-ruby-pathname/pathname v0.0.0-20261025180827-dbeb0072806a
	github.com/go-ruby-pg/pg v0.0.0-20261025191721-f5e310f52c5f
	github.com/go-ruby-prawn/prawn v0.0.0-20261028113423-d440480b5471
	github.com/go-ruby-prettyprint/prettyprint v0.0.0-20261027104533-2b51f1b3006a
	github.com/go-ruby-prime/prime v0.0.0-20261025191735-6a72777b3c32
	github.com/go-ruby-protobuf/protobuf v0.0.0-20261025191740-a21ebb162714
	github.com/go-ruby-pstore/pstore v0.0.0-20261025180837-c6ede396c8ad
	github.com/go-ruby-public-suffix/public-suffix v0.0.0-20261025191745-2241d1396beb
	github.com/go-ruby-puma/puma v0.0.0-20261025191750-94271ed51dbf
	github.com/go-ruby-pundit/pundit v0.0.0-20261025191755-90a955c3ade8
	github.com/go-ruby-puppet-resource-api/puppet-resource-api v0.0.0-20261025191800-ecd01f7790ae
	github.com/go-ruby-puppet/puppet v0.0.0-20261025191804-adbb6dd1516d
	github.com/go-ruby-racc/racc v0.0.0-20261025191809-0f671b05efa7
	github.com/go-ruby-rack/rack v0.0.0-20261025191814-3f115d09a2d2
	github.com/go-ruby-rails/rails v0.0.0-20261025191819-f8b89e6e04fd
	github.com/go-ruby-railties/railties v0.0.0-20261025191825-e6078d8fd8de
	github.com/go-ruby-rake/rake v0.0.0-20261025191830-feaef7916821
	github.com/go-ruby-ransack/ransack v0.0.0-20261025191835-4bb0c90b22bd
	github.com/go-ruby-rdoc/rdoc v0.0.0-20261025191845-0a041c5257b8
	github.com/go-ruby-redis/redis v0.0.0-20261025191855-6267a6f1b991
	github.com/go-ruby-regexp/regexp v0.1.1
	github.com/go-ruby-reline/reline v0.1.0
	github.com/go-ruby-resolv/resolv v0.0.0-20261027105700-a36dcee9cbce
	github.com/go-ruby-resque/resque v0.0.0-20261025191914-a0f00500072e
	github.com/go-ruby-rexml/rexml v0.0.0-20261025191919-14c852663820
	github.com/go-ruby-roda/roda v0.0.0-20261027105846-ceed5c054ab7
	github.com/go-ruby-rolify/rolify v0.0.0-20261025191928-26a402de4a07
	github.com/go-ruby-rqrcode/rqrcode v0.0.0-20261025191938-5ba1efd216ff
	github.com/go-ruby-rspec/rspec v0.0.0-20261025191943-fa093a53174f
	github.com/go-ruby-rss/rss v0.0.0-20261025191948-f4e2db08540f
	github.com/go-ruby-rubocop/rubocop v0.0.0-20261027110214-1e3b41167fcb
	github.com/go-ruby-rubygems/rubygems v0.0.0-20261025191958-56a2a629c1e1
	github.com/go-ruby-saml/saml v0.0.0-20261025192003-cb9404be2bac
	github.com/go-ruby-sass/sass v0.0.0-20261004225654-93da1ddfb811
	github.com/go-ruby-scanf/scanf v0.0.0-20261025192009-fa248a2960ea
	github.com/go-ruby-securerandom/securerandom v0.0.0-20261025192014-28ba9d3ceafc
	github.com/go-ruby-semantic-puppet/semantic-puppet v0.0.0-20261025192019-bbfa4f610759
	github.com/go-ruby-sequel/sequel v0.0.0-20261025192024-f5d036e4a59d
	github.com/go-ruby-shellwords/shellwords v0.0.0-20261025192034-a550a18cadf1
	github.com/go-ruby-shrine/shrine v0.0.0-20261025192039-fd5368289bcd
	github.com/go-ruby-sidekiq/sidekiq v0.0.0-20261025192044-2a6838452878
	github.com/go-ruby-simplecov/simplecov v0.0.0-20261025192049-9599557a7704
	github.com/go-ruby-sinatra/sinatra v0.0.0-20261025192054-f73208d24625
	github.com/go-ruby-slim/slim v0.0.0-20261027111030-834d1fa0a20d
	github.com/go-ruby-sodium/sodium v0.0.0-20261025192104-fb3f343b3570
	github.com/go-ruby-sqlite3/sqlite3 v0.0.0-20261025192109-9b7f005bb831
	github.com/go-ruby-strscan/strscan v0.0.0-20261025192126-33c56df4adaf
	github.com/go-ruby-thor/thor v0.0.0-20261025192136-7596411920dd
	github.com/go-ruby-timecop/timecop v0.0.0-20261027111625-77d8630246cb
	github.com/go-ruby-toml/toml v0.0.0-20261025192150-01bc55ce2f7c
	github.com/go-ruby-tsort/tsort v0.0.0-20261025192155-fe2d787cda87
	github.com/go-ruby-typhoeus/typhoeus v0.0.0-20261025192200-790cfd4c71f5
	github.com/go-ruby-tzinfo/tzinfo v0.0.0-20261025192205-b649fd1ac7cd
	github.com/go-ruby-unicode-normalize/unicode-normalize v0.1.0
	github.com/go-ruby-uri/uri v0.0.0-20261025192215-07bbf3955f9f
	github.com/go-ruby-vcr/vcr v0.0.0-20261025192220-6f3daa26b818
	github.com/go-ruby-warden/warden v0.0.0-20261025192225-adaf634733a9
	github.com/go-ruby-webauthn/webauthn v0.0.0-20261025192231-a92d75289dd4
	github.com/go-ruby-webmock/webmock v0.0.0-20261025192236-99a7562b0e75
	github.com/go-ruby-webrick/webrick v0.0.0-20261025192241-a82f0297760e
	github.com/go-ruby-widgets/mvvm v0.3.0
	github.com/go-ruby-widgets/tui v0.5.0
	github.com/go-ruby-widgets/widgets v0.13.0
	github.com/go-ruby-yaml/yaml v0.1.0
	github.com/go-ruby-zeitwerk/zeitwerk v0.0.0-20261025192257-94b5b3897969
	github.com/go-ruby-zlib/zlib v0.0.0-20261025192302-bf1a9eacebda
	github.com/go-statistics/stats v0.1.0
	github.com/go-webauthn/webauthn v0.18.2
	github.com/go-xslt/xslt v0.1.0
	github.com/nats-io/nats-server/v2 v2.15.2
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
	github.com/andybalholm/brotli v1.2.7 // indirect
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
	github.com/go-opentype/opentype v0.16.1 // indirect
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
	github.com/nats-io/nats.go v1.55.0 // indirect
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
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20261009151841-97e8ea77aa2a // indirect
	golang.org/x/term v0.47.0 // indirect
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
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.61.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
