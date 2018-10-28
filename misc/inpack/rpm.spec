%define app_home    /opt/sysinner/insoho
%define app_user    action
%define app_user_id 2048

Name:    insoho
Version: __version__
Release: __release__%{?dist}
Vendor:  sysinner.com
Summary: Productivity Tools for Small Office or Home Office
License: Apache 2
Group:   Applications

Source0:   %{name}-__version__.tar.gz
BuildRoot: %{_tmppath}/%{name}-%{version}-%{release}

Requires:       redhat-lsb-core
Requires(pre):  shadow-utils
Requires(post): chkconfig

%description
%prep
%setup -q -n %{name}-%{version}
%build

%install
rm -rf %{buildroot}
mkdir -p %{buildroot}%{app_home}/
mkdir -p %{buildroot}/lib/systemd/system/

cp -rp * %{buildroot}%{app_home}/
# install -m 755 bin/insoho                   %{buildroot}%{app_home}/bin/insoho
# install -m 755 bin/inagent                  %{buildroot}%{app_home}/bin/inagent
# install -m 755 bin/ininit                   %{buildroot}%{app_home}/bin/ininit
# install -m 755 bin/docker2oci               %{buildroot}%{app_home}/bin/docker2oci
install -m 644 etc/empty.tpl.json           %{buildroot}%{app_home}/etc/config.json
install -m 644 etc/empty.tpl.json           %{buildroot}%{app_home}/etc/inpack_config.json
install -m 600 misc/systemd/systemd.service %{buildroot}/lib/systemd/system/insoho.service

%clean
rm -rf %{buildroot}

%pre
getent passwd %{app_user} >/dev/null || \
    useradd -d /home/%{app_user} -s /sbin/nologin -u %{app_user_id} %{app_user}
exit 0

%post
systemctl daemon-reload

%preun

%postun

%files
%defattr(-,root,root,-)
%dir %{app_home}
%config(noreplace) %{app_home}/etc/config.json
%config(noreplace) %{app_home}/etc/inpack_config.json
/lib/systemd/system/insoho.service

%{app_home}/

