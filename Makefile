#
# Makefile for testing pion echo example
#
PROG=pion-echo
#----------------------------------------------------------------------------------
usage:
	@echo "usage: make [build|run]"

edit e:
	vi main.go

build b: *.go
	go build -o $(PROG) *.go

run r:
	./$(PROG) -vcodec=VP9 

kill k:
	pkill $(PROG)

clean c:
	rm -f $(PROG)

open o:
	open http://localhost:8080/

search s:
	hub-search --lang=go "pion webrtc"
#----------------------------------------------------------------------------------
git g:
	@echo "> make (git:g) [update|store]"
git-update gu:
	git add .
	git commit -a -m "update inforamtion"
	git push
git-store gs:
	git config credential.helper store
#----------------------------------------------------------------------------------

