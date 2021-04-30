# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.2.4] 2021-04-30

### Added

* Builds for `arm`, `arm64`, `ppc64le`

## [v0.2.3] 2021-01-20

### Added
* Set `nodeName` on endpoints if `K8S_AWAIT_ELECTION_NODE_NAME` is set. This enables Kubernetes to route traffic
  from the leader Pod to itself via the service.

## [v0.2.2] 2021-01-12

### Changed
* Fixed golang version download for releases

## [v0.2.1] 2021-01-12

### Changed
* Used golang 1.15 to build releases

## [v0.2.0] 2020-08-20

### Added
* Ability to update service endpoints based on current leader

### Changed
* health endpoint now reports error on expired lease

## [v0.1.0] 2020-07-29

### Added
* Running in elected mode in k8s cluster
* Running without election outside of k8s cluster
* Basic error handling

[Unreleased]: https://github.com/LINBIT/k8s-await-election/compare/v0.2.4...HEAD
[v0.2.4]: https://github.com/LINBIT/k8s-await-election/compare/v0.2.3...v0.2.4
[v0.2.3]: https://github.com/LINBIT/k8s-await-election/compare/v0.2.2...v0.2.3
[v0.2.2]: https://github.com/LINBIT/k8s-await-election/compare/v0.2.1...v0.2.2
[v0.2.1]: https://github.com/LINBIT/k8s-await-election/compare/v0.2.0...v0.2.1
[v0.2.0]: https://github.com/LINBIT/k8s-await-election/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/LINBIT/k8s-await-election/commits/v0.1.0
