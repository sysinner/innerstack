%define app_home    /opt/los/soho
%define app_user    action
%define app_user_id 2048

Name:    los-soho
Version: __version__
Release: __release__%{?dist}
Vendor:  lessOS.com
Summary: Productivity Tools for Small Office or Home Office
License: Apache 2
Group:   Applications

Source0:   %{name}-__version__.tar.gz
BuildRoot: %{_tmppath}/%{name}-%{version}-%{release}

Requires:       redhat-lsb-core
Requires:       docker-engine
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
cp -rp etc/config.json.tpl %{buildroot}%{app_home}/etc/config.json
install -m 600 misc/lospack/crond %{buildroot}/etc/cron.d/los-soho

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
%config            /etc/cron.d/los-soho

%{app_home}/

