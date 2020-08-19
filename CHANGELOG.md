# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
* Ability to update service endpoints based on current leader

### Changed
* health endpoint now reports error on expired lease

## [v0.1.0] 2020-07-29

### Added
* Running in elected mode in k8s cluster
* Running without election outside of k8s cluster
* Basic error handling
