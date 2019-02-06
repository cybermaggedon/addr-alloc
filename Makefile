
VERSION=$(shell git describe | sed 's/^v//')

CONTAINER=gcr.io/trust-networks/addr-alloc:${VERSION}

GOFILES=addr_alloc

all: godeps ${GOFILES} container

GODEPS=go/.bolt

%: %.go ${GODEPS}
	GOPATH=$$(pwd)/go go build $<

go:
	mkdir go

godeps: go ${GODEPS}

go/.bolt:
	GOPATH=$$(pwd)/go go get github.com/boltdb/bolt
	touch $@

container:
	docker build -t ${CONTAINER} .

push: container
	gcloud docker -- push ${CONTAINER}

# Continuous deployment support
# addr-alloc is referenced in the vpn-service
BRANCH=master
PREFIX=resources/vpn-service
FILE=${PREFIX}/ksonnet/addr-alloc-version.jsonnet
REPO=git@github.com:trustnetworks/vpn-service

tools: phony
	if [ ! -d tools ]; then \
		git clone git@github.com:trustnetworks/cd-tools tools; \
	fi; \
	(cd tools; git pull)

phony:

bump-version: tools
	tools/bump-version

update-cluster-config: tools
	tools/update-version-config ${BRANCH} ${VERSION} ${FILE}


