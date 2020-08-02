# Install

## install from YUM (CentOS 7.x, RHEL 7.x, ...)

### step 1: setup yum repo, and install

``` shell
wget https://www.sysinner.com/repo/el/sysinner.repo -O /etc/yum.repos.d/sysinner.repo

yum install -y sysinner-innerstack

systemctl start sysinner-innerstack
```

### step 2: initial configuration

### 2.1 single-node cluster initial configuration

``` shell
/opt/sysinner/innerstack/bin/innerstack-cli config-init --zone_master_enable
```

### 2.2 multi nodes cluster initial configuration

``` shell
# master node (zone-master)
/opt/sysinner/innerstack/bin/innerstack-cli config-init --zone_master_enable --multi_host_enable

# member node
/opt/sysinner/innerstack/bin/innerstack-cli config-init
```

Tips: use ```innerstack-cli config-init --help``` to get more options to initial the zone-master  


### 2.3 install the container driver, start it, and pull the general image

``` shell
cd /opt/sysinner/innerstack/misc/install
./install.sh


cd /opt/sysinner/innerstack/misc/boximages/docker/g2
./build
```


### step 3: entry of user panel, system management

* user panel:  http://your-ip-address:9530/in/
* operation panel:  http://your-ip-address:9530/in/ops/
* detailed description please see: https://www.sysinner.com/doc/

