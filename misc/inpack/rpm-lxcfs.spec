%define app_home /opt/sysinner/innerstack

Name:    sysinner-innerstack-lxcfs
Version: __version__
Release: __release__%{?dist}
Vendor:  sysinner.com
Summary: Enterprise PaaS Engine LXCFS
License: Apache 2
Group:   Applications

Source0:   %{name}-__version__.tar.gz
BuildRoot: %{_tmppath}/%{name}-%{version}-%{release}

Requires:       redhat-lsb-core
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
mkdir -p %{buildroot}%{app_home}/bin
mkdir -p %{buildroot}%{app_home}/lib/lxcfs
mkdir -p %{buildroot}/lib/systemd/system
mkdir -p %{buildroot}/var/lib/innerstack-lxcfs

install deps/lxcfs/src/lxcfs %{buildroot}%{app_home}/bin/innerstack-lxcfs
install -m 640 deps/lxcfs/src/.libs/liblxcfs.so %{buildroot}%{app_home}/lib/lxcfs/liblxcfs.so
install -m 600 misc/systemd/lxcfs.service %{buildroot}/lib/systemd/system/innerstack-lxcfs.service

rm -fr %{buildroot}%{app_home}/deps/

%clean
rm -rf %{buildroot}

%pre

%post
systemctl daemon-reload

%preun

%postun

%files
%defattr(-,root,root,-)
%dir /var/lib/innerstack-lxcfs
%{app_home}/lib/lxcfs/liblxcfs.so
%{app_home}/bin/innerstack-lxcfs
/lib/systemd/system/innerstack-lxcfs.service

