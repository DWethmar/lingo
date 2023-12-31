#!/bin/bash

mkdir -p certs

openssl req -x509 -newkey rsa:4096 -sha256 -days 3650 -nodes \
    -keyout certs/lingo.key -out certs/lingo.crt -subj '/CN=lingo' \
    -extensions san \
    -config <(echo '[req]'; echo 'distinguished_name=req'; echo '[san]'; echo 'subjectAltName=DNS:lingo')