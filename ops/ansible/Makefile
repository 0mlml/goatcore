# goat — Ansible deployment targets
#
# Prerequisites:
#   - aws-vault configured (legacy profile)
#   - terraform init + apply already run
#   - brew install ansible

SHELL := /bin/bash
AWS_CMD := aws-vault exec legacy --no-session --no-session --
REPO_DIR := $(shell cd ../.. && pwd)
TF_DIR := $(REPO_DIR)/terraform
SERVE_DIR := $(REPO_DIR)/ops/serve
SERVE_VENV := $(SERVE_DIR)/.venv

.PHONY: deploy inventory relay-servers nethost-relays verify terraform serve serve-generate infra services help

help: ## Show available targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

deploy: inventory relay-servers nethost-relays verify ## Full deployment: inventory → VPS bootstrap → NETHOST config → verify

inventory: ## Generate Ansible inventory from Terraform outputs
	@echo "==> Generating inventory from Terraform..."
	inventory/generate.sh

terraform: ## Run Terraform plan + apply
	@echo "==> Running Terraform..."
	cd $(TF_DIR) && $(AWS_CMD) terraform apply -input=false -auto-approve

relay-servers: ## Bootstrap VPS relays (rathole + Coturn + NB relay)
	@echo "==> Bootstrapping VPS relays..."
	ansible-playbook relay-server.yml \
		$(if $(TURN_SECRET),-e shared_turn_secret=$(TURN_SECRET)) \
		$(if $(RELAY_SECRET),-e nb_relay_auth_secret=$(RELAY_SECRET)) \
		$(if $(LIMIT),--limit $(LIMIT))

nethost-relays: ## Configure NETHOST rathole clients + STUN/relay registration
	@echo "==> Configuring NETHOST relay clients..."
	ansible-playbook nethost-relay.yml \
		$(if $(RELAY_SECRET),-e nb_relay_auth_secret=$(RELAY_SECRET)) \
		$(if $(ASK_PASS),--ask-become-pass)

site: ## Run full site.yml (all tiers)
	ansible-playbook site.yml \
		$(if $(TURN_SECRET),-e shared_turn_secret=$(TURN_SECRET)) \
		$(if $(RELAY_SECRET),-e nb_relay_auth_secret=$(RELAY_SECRET))

infra: ## Tier 1: VPS relays + NETHOST infrastructure
	ansible-playbook site-infra.yml \
		$(if $(TURN_SECRET),-e shared_turn_secret=$(TURN_SECRET)) \
		$(if $(RELAY_SECRET),-e nb_relay_auth_secret=$(RELAY_SECRET))

services: ## Tier 2: NETHOST hosted services (Caddy, status, smoketest, secwatch, demogame)
	ansible-playbook site-services.yml

serve-generate: ## Regenerate Caddy configs from services.yaml
	@test -d $(SERVE_VENV) || python3 -m venv $(SERVE_VENV)
	@$(SERVE_VENV)/bin/pip install -q -r $(SERVE_DIR)/requirements.txt
	@$(SERVE_VENV)/bin/python3 $(SERVE_DIR)/generate.py

serve: serve-generate ## Generate configs + deploy service-hosting layer
	@echo "==> Deploying service-hosting layer..."
	ansible-playbook nethost-serve.yml \
		$(if $(ASK_PASS),--ask-become-pass)

verify: ## Verify relay connectivity (curl + nc tests)
	@echo "==> Verifying relays..."
	@cd $(TF_DIR) && \
	RELAY_OUTPUT=$$($(AWS_CMD) terraform output -json relays 2>/dev/null) && \
	for name in $$(echo "$$RELAY_OUTPUT" | python3 -c "import sys,json; print(' '.join(json.load(sys.stdin).keys()))"); do \
		ip=$$(echo "$$RELAY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['$$name']['public_ip'])"); \
		mgmt=$$(curl -sk --connect-timeout 5 --resolve "netbird.legacy.example:443:$$ip" "https://netbird.legacy.example/api/accounts" 2>/dev/null | grep -q 401 && echo OK || echo FAIL); \
		ctl=$$(nc -z -w 3 "$$ip" 2333 2>/dev/null && echo OK || echo FAIL); \
		printf "  %-4s %-15s  mgmt=%s  rathole=%s\n" "$$name" "$$ip" "$$mgmt" "$$ctl"; \
	done
