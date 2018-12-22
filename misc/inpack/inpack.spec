[project]
name = insoho
version = 0.4.0.alpha.5
vendor = hooto.com
homepage = https://github.com/sysinner/insoho
groups = dev/sys-srv

%build
export PATH=$PATH:/usr/local/go/bin:/opt/gopath/bin
export GOPATH=/opt/gopath

mkdir -p {{.buildroot}}/etc
mkdir -p {{.buildroot}}/bin
mkdir -p {{.buildroot}}/misc/inpack
mkdir -p {{.buildroot}}/var/log
mkdir -p {{.buildroot}}/var/tmp
mkdir -p {{.buildroot}}/var/inpack_database
mkdir -p {{.buildroot}}/var/inpack_storage
mkdir -p {{.buildroot}}/webui/in

go build -ldflags "-X main.version={{.project__version}} -X main.release={{.project__release}}" -o {{.buildroot}}/bin/insoho cmd/server/main.go
go build -ldflags "-s -w -X main.version={{.project__version}} -X main.release={{.project__release}}" -o {{.buildroot}}/bin/inagent  cmd/inagent/main.go
go build -o {{.buildroot}}/bin/docker2oci github.com/coolljt0725/docker2oci

install bin/ininit {{.buildroot}}/bin/ininit
install -m 644 etc/config.tpl.json {{.buildroot}}/etc/config.tpl.json
install -m 644 etc/empty.tpl.json  {{.buildroot}}/etc/inpack_config.tpl.json
install -m 644 etc/empty.tpl.json  {{.buildroot}}/etc/empty.tpl.json

sed -i 's/debug:\!0/debug:\!1/g' {{.buildroot}}/webui/in/cp/js/main.js
sed -i 's/debug:\!0/debug:\!1/g' {{.buildroot}}/webui/in/ops/js/main.js
sed -i 's/debug:\!0/debug:\!1/g' {{.buildroot}}/vendor/github.com/hooto/iam_static/webui/iam/js/main.js
sed -i 's/debug:true/debug:false/g' {{.buildroot}}/webui/in/cp/js/main.js
sed -i 's/debug:true/debug:false/g' {{.buildroot}}/webui/in/ops/js/main.js
sed -i 's/debug:true/debug:false/g' {{.buildroot}}/vendor/github.com/hooto/iam_static/webui/iam/js/main.js

rm -rf /tmp/rpmbuild/*
mkdir -p /tmp/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS,BUILDROOT}

mkdir -p /tmp/rpmbuild/SOURCES/insoho-{{.project__version}}/
rsync -av {{.buildroot}}/* /tmp/rpmbuild/SOURCES/insoho-{{.project__version}}/

sed -i 's/__version__/{{.project__version}}/g' /tmp/rpmbuild/SOURCES/insoho-{{.project__version}}/misc/inpack/rpm.spec
sed -i 's/__release__/{{.project__release}}/g' /tmp/rpmbuild/SOURCES/insoho-{{.project__version}}/misc/inpack/rpm.spec

cd /tmp/rpmbuild/SOURCES/
tar zcf insoho-{{.project__version}}.tar.gz insoho-{{.project__version}}

rpmbuild --define "debug_package %{nil}" -ba /tmp/rpmbuild/SOURCES/insoho-{{.project__version}}/misc/inpack/rpm.spec \
  --define='_tmppath /tmp/rpmbuild' \
  --define='_builddir /tmp/rpmbuild/BUILD' \
  --define='_topdir /tmp/rpmbuild' \
  --define='dist .{{.project__dist}}'

%files
misc/
bin/
webui/in/cp/
webui/in/ops/
webui/in/hl/
webui/in/cm/
webui/in/lessui/js
webui/in/lessui/css
webui/in/lessui/img
webui/in/purecss/
webui/in/twbs/
webui/in/open-iconic/
webui/in/fa/
webui/about.tpl
webui/ips/ips/
webui/hchart/webui/chartjs/
websrv/mgr/views/
vendor/github.com/hooto/iam_static/websrv/views/
vendor/github.com/hooto/iam_static/webui/

%js_compress
webui/in/cp/js/
webui/in/ops/js/
webui/in/twbs/4.0/js/
webui/in/jquery/
webui/in/cm/
webui/ips/ips/js/
webui/hchart/webui/chartjs/
webui/hchart/webui/hchart.js
vendor/github.com/hooto/iam_static/webui/

%css_compress
webui/in/cp/css/
webui/in/ops/css/
webui/in/twbs/4.0/css/
webui/in/cm/
webui/in/fa/
webui/ips/ips/css/
vendor/github.com/hooto/iam_static/webui/

%html_compress
websrv/mgr/views/
webui/ips/ips/tpl/
vendor/github.com/hooto/iam_static/websrv/views/
vendor/github.com/hooto/iam_static/webui/tpl/

%png_compress

