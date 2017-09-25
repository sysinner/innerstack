project.name = los-soho
project.version = 0.2.1.dev
project.vendor = hooto.com
project.homepage = https://github.com/lessos/los-soho
project.groups = dev/sys-srv

%build
export PATH=$PATH:/usr/local/go/bin:/opt/gopath/bin
export GOPATH=/opt/gopath

mkdir -p {{.buildroot}}/etc
mkdir -p {{.buildroot}}/bin
mkdir -p {{.buildroot}}/misc/lospack
mkdir -p {{.buildroot}}/var/{log,tmp,lps_storage}
mkdir -p {{.buildroot}}/webui/los

go build -ldflags "-s -w -X main.Released=`date -u '+%Y-%m-%d_%I:%M:%S%p'`" -o {{.buildroot}}/bin/los-soho cmd/server/main.go
go build -ldflags "-s -w" -o {{.buildroot}}/bin/lpagent  cmd/lpagent/main.go
go build -ldflags "-s -w" -o {{.buildroot}}/bin/los-opcli  cmd/opcli/main.go

install bin/lpinit {{.buildroot}}/bin/lpinit
install bin/keeper {{.buildroot}}/bin/keeper
install -m 644 etc/config.tpl.json {{.buildroot}}/etc/config.tpl.json
install -m 644 vendor/github.com/lessos/lospack/etc/lps_config.tpl.json {{.buildroot}}/etc/lps_config.tpl.json

sed -i 's/debug:\!0/debug:\!1/g' {{.buildroot}}/webui/los/cp/js/main.js
sed -i 's/debug:\!0/debug:\!1/g' {{.buildroot}}/webui/los/ops/js/main.js
sed -i 's/debug:\!0/debug:\!1/g' {{.buildroot}}/vendor/github.com/hooto/iam/webui/iam/js/main.js

rm -rf /tmp/rpmbuild/*
mkdir -p /tmp/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS,BUILDROOT}

mkdir -p /tmp/rpmbuild/SOURCES/los-soho-{{.project__version}}/
rsync -av {{.buildroot}}/* /tmp/rpmbuild/SOURCES/los-soho-{{.project__version}}/

sed -i 's/__version__/{{.project__version}}/g' /tmp/rpmbuild/SOURCES/los-soho-{{.project__version}}/misc/lospack/rpm.spec
sed -i 's/__release__/{{.project__release}}/g' /tmp/rpmbuild/SOURCES/los-soho-{{.project__version}}/misc/lospack/rpm.spec

cd /tmp/rpmbuild/SOURCES/
tar zcf los-soho-{{.project__version}}.tar.gz los-soho-{{.project__version}}

rpmbuild -ba /tmp/rpmbuild/SOURCES/los-soho-{{.project__version}}/misc/lospack/rpm.spec \
  --define='_tmppath /tmp/rpmbuild' \
  --define='_builddir /tmp/rpmbuild/BUILD' \
  --define='_topdir /tmp/rpmbuild' \
  --define='dist .{{.project__dist}}'

%files
misc/
bin/
webui/los/cp/
webui/los/ops/
webui/los/hl/
webui/los/lessui/js
webui/los/lessui/css
webui/los/lessui/img
webui/los/purecss/
webui/los/twbs/
webui/lps/lps/
webui/hchart/webui/chartjs/
websrv/mgr/views/
vendor/github.com/hooto/iam/websrv/views/
vendor/github.com/hooto/iam/webui/

%js_compress
webui/los/cp/js/
webui/los/ops/js/
webui/los/twbs/3.3/js/
webui/lps/lps/js/
webui/hchart/webui/chartjs/
webui/hchart/webui/hchart.js
vendor/github.com/hooto/iam/webui/iam/js/

%css_compress
webui/lps/lps/css/
vendor/github.com/hooto/iam/webui/iam/css/

%html_compress
websrv/mgr/views/
webui/lps/lps/tpl/
vendor/github.com/hooto/iam/websrv/views/
vendor/github.com/hooto/iam/webui/tpl/

%png_compress
