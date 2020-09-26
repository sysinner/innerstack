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
Requires(pre):  shadow-utils
Requires(post): chkconfig

%description

%prep

%setup -q -n %{name}-%{version}

%build


#yes | yum install autotools-dev m4 autoconf2.13 autobook autoconf-archive gnu-standards autoconf-doc libtool
#yes | yum install "fuse-devel.$(uname -p)"
#yes | yum install "pam-devel.$(uname -p)"
#yes | yum install "fuse.$(uname -p)"

cd deps/lxcfs
./bootstrap.sh
./configure --prefix=/opt/sysinner/innerstack 
make -j4


%install

rm -rf %{buildroot}
mkdir -p %{buildroot}%{app_home}/lib/lxcfs
mkdir -p %{buildroot}/lib/systemd/system/
# mkdir -p %{buildroot}/usr/local/bin/
mkdir -p %{buildroot}/var/lib/innerstack-lxcfs/

cp -rp * %{buildroot}%{app_home}/
# install -m 755 bin/innerstack                   %{buildroot}%{app_home}/bin/innerstack
# install -m 755 bin/inagent                  %{buildroot}%{app_home}/bin/inagent
# install -m 755 bin/ininit                   %{buildroot}%{app_home}/bin/ininit
# install -m 755 bin/docker2oci               %{buildroot}%{app_home}/bin/docker2oci
install deps/lxcfs/src/lxcfs %{buildroot}%{app_home}/bin/innerstack-lxcfs
install -m 640 deps/lxcfs/src/.libs/liblxcfs.so %{buildroot}%{app_home}/lib/lxcfs/liblxcfs.so
install -m 600 misc/systemd/systemd.service %{buildroot}/lib/systemd/system/innerstack.service
install -m 600 misc/systemd/lxcfs.service %{buildroot}/lib/systemd/system/innerstack-lxcfs.service

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
%dir /var/lib/innerstack-lxcfs/
/lib/systemd/system/innerstack.service
/lib/systemd/system/innerstack-lxcfs.service

%{app_home}/

