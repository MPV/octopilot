---
title: "Introduction"
anchor: "intro"
weight: 10
---

**Octopilot** is a CLI tool designed to help you automate your Gitops workflow, by automatically creating and merging GitHub Pull Requests to update specific content in Git repositories.

It works by:
- cloning one or more [repositories](#repos), defined either:
  - [statically](#static)
  - [dynamically](#dynamic), using environment variables or GitHub search queries
- running one or more [updaters](#updaters) on each cloned repository, using either:
  - the [YAML updater](#yaml), to quickly update YAML files
  - the [YQ updater](#yq), based on [mikefarah's yq](https://github.com/mikefarah/yq), to manipulate YAML or JSON files as you want
  - the [Helm updater](#helm), to easily update the dependencies of an [Helm](https://helm.sh/) chart
  - The [sops updater](#sops), to manipulate files encrypted with [mozilla's sops](https://github.com/mozilla/sops)
  - The [regex updater](#regex), to update any kind of text file using a regular expression
  - The [exec updater](#exec), to execute any command you want
- [commit/push](#commit) the changes
- create [Pull Requests](#pull-request) and optionally merge them

If you want to see what you can do with Octopilot for real, here is a set of real-world [use-cases](#use-cases) that we have at [Dailymotion](https://www.dailymotion.com/):
- [Promoting a new application release](#use-case-app-promotion) with a gitops workflow
- [Updating certificates](#use-case-update-certs) with a gitops workflow
- [Updating Go dependencies](#use-case-go-deps)
- [Previsualizing changes](#use-case-preview) done by octopilot, without pushing to the remote GitHub repository