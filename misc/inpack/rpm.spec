%define app_home    /opt/sysinner/innerstack
%define app_user    action
%define app_user_id 2048

Name:    sysinner-innerstack
Version: __version__
Release: __release__%{?dist}
Vendor:  sysinner.com
Summary: Enterprise PaaS Engine
License: Apache 2
Group:   Applications

Source0:   %{name}-__version__.tar.gz
BuildRoot: %{_tmppath}/%{name}-%{version}-%{release}

Requires:       redhat-lsb-core
Requires:       rsync
Requires:       fuse-libs
Requires:       docker-ce
Requires:       psmisc
Requires:       rsync
Requires:       net-tools
Requires:       device-mapper-persistent-data
Requires:       lvm2
Requires:       sysinner-innerstack-lxcfs
Requires(pre):  shadow-utils
Requires(post): chkconfig

%description

%prep

%setup -q -n %{name}-%{version}

%build


%install

rm -rf %{buildroot}
mkdir -p %{buildroot}/lib/systemd/system
# mkdir -p %{buildroot}/usr/local/bin/

mkdir -p %{buildroot}%{app_home}/

cp -rp * %{buildroot}%{app_home}/
# install -m 755 bin/innerstack                   %{buildroot}%{app_home}/bin/innerstack
# install -m 755 bin/inagent                  %{buildroot}%{app_home}/bin/inagent
# install -m 755 bin/ininit                   %{buildroot}%{app_home}/bin/ininit
# install -m 755 bin/docker2oci               %{buildroot}%{app_home}/bin/docker2oci
install -m 600 misc/systemd/systemd.service %{buildroot}/lib/systemd/system/innerstack.service

rm -fr %{buildroot}%{app_home}/deps/

%clean
rm -rf %{buildroot}

%pre
getent passwd %{app_user} >/dev/null || \
    useradd -d /home/%{app_user} -s /sbin/nologin -u %{app_user_id} %{app_user}
exit 0

%post
ln -s -f %{app_home}/bin/innerstack /usr/local/bin/innerstack
systemctl daemon-reload

%preun

%postun
rm -f /usr/local/bin/innerstack

%files
%defattr(-,root,root,-)
%dir %{app_home}
/lib/systemd/system/innerstack.service

%{app_home}/

