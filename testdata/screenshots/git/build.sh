#!/usr/bin/env bash
# Builds the Git repository of the Source Control screenshots of README.md (scripts/screenshots.sh):
#
#   build.sh <dir> clean|changes
#
# <dir> must not exist. It gets a small Go project, "shop", with a history of three authors (the dates are counted back from
# today, so the pictures say "2 days ago" and "2 months ago" whenever they are made), two branches, a merge, the tag v0.2.0 and
# a bare origin next to it (<dir>.origin.git, 1 commit behind the local main). "changes" adds, on top, a change in every group of
# the Source Control view: staged, unstaged, a deleted and a renamed file, and an untracked one.
#
# The real git runs in an environment made from scratch (no HOME, no global or system configuration), so nothing of the
# user's git configuration is read or written.
set -euo pipefail

[[ $# -eq 2 && ( "$2" == clean || "$2" == changes ) ]] || { echo "usage: $0 <dir> clean|changes" >&2; exit 64; }
dir="$1"
mode="$2"
mkdir -p "$(dirname "$dir")"
mkdir "$dir"
dir="$(cd "$dir" && pwd)"
origin="$dir.origin.git"

export HOME="$dir.home" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat LC_ALL=C
mkdir "$HOME"
trap 'rm -rf "$HOME"' EXIT

cd "$dir"
git init -q -b main
git config user.name "Ada Example"
git config user.email "ada@example.com"

# commit <days ago> <author> <message>: commits what is staged, by the author, that many days ago.
commit() {
  local days="$1" who="$2" msg="$3" when
  when="$(date -d "$days days ago 10:30" --iso-8601=seconds)"
  GIT_AUTHOR_NAME="$who Example" GIT_AUTHOR_EMAIL="$(tr 'A-Z' 'a-z' <<<"$who")@example.com" \
    GIT_COMMITTER_NAME="$who Example" GIT_COMMITTER_EMAIL="$(tr 'A-Z' 'a-z' <<<"$who")@example.com" \
    GIT_AUTHOR_DATE="$when" GIT_COMMITTER_DATE="$when" git commit -q -m "$msg"
}

mkdir -p store docs

cat >go.mod <<'GO'
module example.com/shop

go 1.22
GO
cat >README.md <<'MD'
# Shop

The orders of a small shop, kept in memory.

## Run

    go run .
MD
cat >main.go <<'GO'
package main

import (
	"fmt"

	"example.com/shop/store"
)

// main adds an order and prints the list.
func main() {
	s := store.New()
	s.Add("tea", 450)
	fmt.Println(s.List())
}
GO
git add -A
commit 60 Ada "Start the shop"

cat >store/store.go <<'GO'
// Package store keeps the orders of the shop.
package store

import (
	"errors"
	"sync"
)

// ErrNotFound is returned when no order has the id asked for.
var ErrNotFound = errors.New("order not found")

// Order is one purchase.
type Order struct {
	ID    int
	Item  string
	Cents int
}

// Store keeps the orders in memory.
type Store struct {
	mu     sync.Mutex
	orders map[int]Order
	next   int
}

// New returns an empty store.
func New() *Store {
	return &Store{orders: map[int]Order{}, next: 1}
}

// Add saves an order and returns it with its id.
func (s *Store) Add(item string, cents int) Order {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := Order{ID: s.next, Item: item, Cents: cents}
	s.orders[o.ID] = o
	s.next++
	return o
}

// Get returns the order with the id.
func (s *Store) Get(id int) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	return o, nil
}
GO
git add -A
commit 52 Ada "Add the store"

sed -i 's|^\t"errors"$|\t"errors"\n\t"sort"|' store/store.go
cat >>store/store.go <<'GO'

// List returns the orders, the oldest first.
func (s *Store) List() []Order {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]Order, 0, len(s.orders))
	for _, o := range s.orders {
		list = append(list, o)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}
GO
git add -A
commit 45 Bo "List the orders in the order they came"

cat >store/store_test.go <<'GO'
package store

import "testing"

func TestAddAndGet(t *testing.T) {
	s := New()
	o := s.Add("tea", 450)
	got, err := s.Get(o.ID)
	if err != nil || got != o {
		t.Fatalf("Get(%d) = %v, %v", o.ID, got, err)
	}
}

func TestGetMissing(t *testing.T) {
	if _, err := New().Get(7); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
GO
git add -A
commit 40 Cy "Test the store"

git switch -q -c feature/discounts
cat >store/discount.go <<'GO'
package store

// Discount takes a percent off a price in cents, rounding down.
func Discount(cents, percent int) int {
	return cents - cents*percent/100
}
GO
git add -A
commit 33 Bo "Add discount rules"
cat >docs/discounts.md <<'MD'
# Discounts

`store.Discount(cents, percent)` takes a percent off a price. The result is
rounded down to whole cents, so a discount never costs the shop a fraction.
MD
git add -A
commit 28 Bo "Document the discount rules"
git switch -q main

cat >>store/store.go <<'GO'

// Total returns the sum of the orders in cents.
func (s *Store) Total() int {
	total := 0
	for _, o := range s.List() {
		total += o.Cents
	}
	return total
}
GO
git add -A
commit 31 Ada "Add Total to the store"

GIT_COMMITTER_DATE="$(date -d "26 days ago 10:30" --iso-8601=seconds)" \
  GIT_AUTHOR_DATE="$(date -d "26 days ago 10:30" --iso-8601=seconds)" \
  git merge -q --no-ff feature/discounts -m "Merge feature/discounts"
git tag -a v0.2.0 -m "Version 0.2.0" HEAD

sed -i 's|^\to, ok := s.orders\[id\]$|\tif id < 1 {\n\t\treturn Order{}, ErrNotFound\n\t}\n\to, ok := s.orders[id]|' store/store.go
git add -A
commit 12 Cy "Refuse an id below one"

git clone -q --bare "$dir" "$origin"
git remote add origin "$origin"
git fetch -q origin
git branch -q -u origin/main main

cat >>README.md <<'MD'

## Test

    go test ./...
MD
git add -A
commit 2 Ada "Say how to run the tests"

if [[ "$mode" == changes ]]; then
  # staged: a modified test and a renamed file
  cat >>store/store_test.go <<'GO'

func TestGetBelowOne(t *testing.T) {
	if _, err := New().Get(0); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
GO
  git mv store/discount.go store/discounts.go
  git add store/store_test.go
  # not staged: store.go (a deleted line, an added check and a changed line, close to each other), main.go, a deleted file, an
  # untracked file
  sed -i '/^\/\/ Add saves an order and returns it with its id\.$/d' store/store.go
  awk '
    { print }
    !done && /^\tdefer s\.mu\.Unlock\(\)$/ {
      print "\tif cents < 0 {"
      print "\t\tcents = 0"
      print "\t}"
      done = 1
    }
  ' store/store.go >store/store.go.new
  mv store/store.go.new store/store.go
  sed -i 's|^\ts\.next++$|\ts.next = o.ID + 1|' store/store.go
  sed -i 's|fmt.Println(s.List())|fmt.Println(s.List(), s.Total())|' main.go
  rm docs/discounts.md
  cat >docs/todo.md <<'MD'
# To do

- Keep the orders in a file.
- Print the total with a decimal point.
MD
fi
echo "built $dir ($mode)"
