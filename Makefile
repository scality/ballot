VERSION ?= $(shell date +%Y%m%d%H%M%S)
SOURCES := $(shell find cmd pkg -name '*go') go.mod go.sum
ARCHIVE := ballot-${VERSION}.tgz
VARIANTS := ballot-${VERSION}-linux-amd64 ballot-${VERSION}-darwin-amd64

TEST_CMD := ginkgo -r -race -cover -failOnPending
TEST_WATCH_CMD := ginkgo watch -r -failFast -race -p -failOnPending

NAME := ballot-${VERSION}
OUTDIR := out
OUTDIR_VERSION := ${OUTDIR}/${NAME}

# only combinations we can test easily for the moment
ARCHES := amd64
OSES := linux darwin

BINARIES := $(foreach bin,${VARIANTS},${OUTDIR_VERSION}/${bin})

all: test binaries

test:
	${TEST_CMD}

test-watch:
	${TEST_WATCH_CMD}

binaries: ${BINARIES}

artifacts: test ${OUTDIR}/${ARCHIVE}

${OUTDIR}/${ARCHIVE}: ${OUTDIR} binaries
	cp README.md ${OUTDIR_VERSION}
	cd ${OUTDIR} && tar zcvf $(shell basename $@) ${NAME}
	@echo Created archive $@

${BINARIES}: ${OUTDIR_VERSION}
	env \
		GOOS=$(shell echo $@ | tr '-' ' ' | awk '{print $$(NF-1)}') \
		GOARCH=$(shell echo $@ | tr '-' ' ' | awk '{print $$(NF)}') \
		go build -o $@ ./cmd/ballot

${OUTDIR} ${OUTDIR_VERSION}:
	mkdir -p $@

clean:
	for f in ${ARCHIVE} ${BINARIES} ; do \
		rm -f ${OUTDIR}/$${f} ; \
	done

.PHONY: test binaries artifacts clean
