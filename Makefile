# Copyright 2019 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
#

PREFIX=/opt/sysinner/innerstack
CC=go
CARGS=build
CFLAGS=""

EXE_DAEMON = bin/innerstackd
EXE_CLI = bin/innerstack
EXE_AGENT = bin/inagent

APP_HOME = /opt/sysinner/innerstack

BUILDCOLOR="\033[34;1m"
BINCOLOR="\033[37;1m"
ENDCOLOR="\033[0m"

ifndef V
	QUIET_BUILD = @printf '%b %b\n' $(BUILDCOLOR)BUILD$(ENDCOLOR) $(BINCOLOR)$@$(ENDCOLOR) 1>&2;
	QUIET_INSTALL = @printf '%b %b\n' $(BUILDCOLOR)INSTALL$(ENDCOLOR) $(BINCOLOR)$@$(ENDCOLOR) 1>&2;
endif


all: build_daemon build_cli build_agent
	@echo ""
	@echo "build complete"
	@echo ""

build_daemon:
	$(QUIET_BUILD)$(CC) $(CARGS) -o $(EXE_DAEMON) ./cmd/server/main.go$(CCLINK)

build_cli:
	$(QUIET_BUILD)$(CC) $(CARGS) -o ${EXE_CLI} cmd/cli/*.go$(CCLINK)

build_agent:
	$(QUIET_BUILD)$(CC) $(CARGS) -o ${EXE_AGENT} cmd/inagent/main.go$(CCLINK)

install: install_init install_bin install_post
	@echo ""
	@echo "install complete"
	@echo ""

install_init:
	$(QUIET_INSTALL)
	mkdir -p ${APP_HOME}/bin
	mkdir -p ${APP_HOME}/etc
	mkdir -p ${APP_HOME}/var/log
	cp -rp misc ${APP_HOME}/ 

install_bin:
	$(QUIET_INSTALL)
	install -m 755 ${EXE_DAEMON} ${APP_HOME}/${EXE_DAEMON}
	install -m 755 ${EXE_CLI} ${APP_HOME}/${EXE_CLI}
	install -m 755 ${EXE_AGENT} ${APP_HOME}/${EXE_AGENT}
	install -m 600 misc/systemd/systemd.service /lib/systemd/system/innerstack.service
	ln -s -f ${APP_HOME}/${EXE_CLI} /usr/local/bin/innerstack

install_post:
	$(QUIET_INSTALL)
	systemctl daemon-reload

clean:
	rm -f ${EXE_DAEMON}
	rm -f ${EXE_CLI}
	rm -f ${EXE_AGENT}
	rm -f /usr/local/bin/innerstack

