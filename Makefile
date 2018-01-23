VERSION := ${shell cat ./VERSION}
DESTDIR :=
PREFIX := /usr
BINDIR=$(PREFIX)/bin
LIBEXECDIR := $(PREFIX)/libexec
LOCALSTATEDIR := /var

SOURCES := $(shell find . 2>&1 | grep -E '.*\.(c|h|go)$$')
PROXY_SOCKET := $(LOCALSTATEDIR)/run/clear-containers/proxy.sock
COMMIT_NO := $(shell git rev-parse HEAD 2> /dev/null || true)
COMMIT := $(if $(shell git status --porcelain --untracked-files=no),${COMMIT_NO}-dirty,${COMMIT_NO})
VERSION_COMMIT := $(if $(COMMIT),$(VERSION)-$(COMMIT),$(VERSION))



#
# Pretty printing
#

V	      = @
Q	      = $(V:1=)
QUIET_GOBUILD = $(Q:@=@echo    '     GOBUILD  '$@;)
QUIET_GEN     = $(Q:@=@echo    '     GEN      '$@;)

# Entry point
all: cc-proxy

#
# proxy
#

cc-proxy: $(SOURCES) Makefile VERSION
	$(QUIET_GOBUILD)go build -i -o $@ -ldflags \
		"-X main.DefaultSocketPath=$(PROXY_SOCKET) -X main.Version=$(VERSION_COMMIT)"

#
# Tests
#

.PHONY: check check-go-static check-go-test
check: check-go-static check-go-test

check-go-static:
	.ci/go-lint.sh

check-go-test:
	.ci/go-test.sh

coverage:
	.ci/go-test.sh html-coverage

#
# Documentation
#

doc:
	$(Q).ci/go-doc.sh || true

#
# install
#

define INSTALL_EXEC
	$(QUIET_INST)install -D $1 $(DESTDIR)$2/$1 || exit 1;

endef
define INSTALL_FILE
	$(QUIET_INST)install -D -m 644 $1 $(DESTDIR)$2/$1 || exit 1;

endef

all-installable: cc-proxy

install: all-installable
	$(call INSTALL_EXEC,cc-proxy,$(LIBEXECDIR)/clear-containers)

clean:
	rm -f cc-proxy

#
# dist
#

dist:
	git archive --format=tar --prefix=cc-proxy-$(VERSION)/ HEAD | xz -c > cc-proxy-$(VERSION).tar.xz
