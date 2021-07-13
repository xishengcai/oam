#!/bin/bash

git diff v100r001c02b013 lsh/master\
--author="$(git config --get user.name)" \
--pretty=tformat: --numstat \
| grep -v 'vendor' \
| grep -v 'swaggerui' \
| grep -v 'go.mod' \
| grep -v 'go.sum' \
| grep -v 'crd' \
| awk '{ add += $1 ; subs += $2 ; loc += $1 + $2 } END { printf "added lines: %s removed lines : %s total lines: %s\n",add,subs,loc }'