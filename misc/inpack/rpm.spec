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
mkdir -p %{buildroot}/etc/cron.d/

cp -rp * %{buildroot}%{app_home}/
install -m 644 etc/config.tpl.json     %{buildroot}%{app_home}/etc/config.json
install -m 644 etc/inpack_config.tpl.json %{buildroot}%{app_home}/etc/inpack_config.json
install -m 600 misc/inpack/crond %{buildroot}/etc/cron.d/insoho

%clean
rm -rf %{buildroot}

%pre
getent passwd %{app_user} >/dev/null || \
    useradd -d /home/%{app_user} -s /sbin/nologin -u %{app_user_id} %{app_user}
exit 0

%post

%preun

%postun

%files
%defattr(-,root,root,-)
%dir %{app_home}
%config(noreplace) %{app_home}/etc/config.json
%config(noreplace) %{app_home}/etc/inpack_config.json
%config            /etc/cron.d/insoho

%{app_home}/

