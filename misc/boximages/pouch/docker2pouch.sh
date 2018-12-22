#!/bin/bash

set -e

TMPDIR=tmpdir
rm -fr $TMPDIR

docker save sysinner:a1el7v1 | ../../../bin/docker2oci $TMPDIR

cd $TMPDIR
sed -i 's/a1el7v1/a2p1el7/g' index.json
tar cf ../a2p1el7.oci.tar .
cd ../

pouch load -i a2p1el7.oci.tar sysinner
rm -fr $TMPDIR

