#!/bin/bash

# Abort if any command fails
set -e

find . -name '*.css' -print | while read line; do
  minify --mime "text/css" -o $line $line
done
