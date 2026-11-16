# deploy/

The production manifests of <https://vibetype.thomas-guettler.de>. Flux on the
cluster applies them directly from this repository, without a detour through a
separate GitOps repository:

1. A green CI run on `main` triggers [release.yml](../.github/workflows/release.yml):
   it tags `v0.0.N`, pushes `ghcr.io/guettli/vibetype:v0.0.N` and force-pushes
   the `deploy` branch = the tagged commit plus one commit that sets `newTag` in
   [kustomization.yaml](kustomization.yaml).
2. Flux polls the `deploy` branch every minute (only this directory) and
   applies it into the `vibetype` namespace.

**This repository is public.** Nothing confidential belongs here, and the app
has no secrets. [scripts/check-deploy.sh](../scripts/check-deploy.sh) (run in
CI) rejects `kind: Secret`, explicit namespaces and every kind other than
Deployment, Service, IngressRoute and Middleware.

Flux applies these manifests with a service account that may only manage those
kinds in the `vibetype` namespace. Whoever can push here cannot reach anything
else in the cluster. The namespace, the PersistentVolumeClaim with the
database, the TLS certificate, DNS, node placement and the Flux objects
themselves live in the private infrastructure repository. To add a new kind
here, extend that Role first.

Roll back by force-pushing the `deploy` branch to an earlier deploy commit, e.g.
`git push --force origin <sha of "deploy v0.0.N">:refs/heads/deploy`. The next
release moves it forward again.
