# Third-party assets

Every asset that Emetos bundles has a folder here, `third_party/<name>/`, with the
license text of the asset as its upstream ships it. `NOTICE` at the repository root
lists all assets, the step that adds each one, and its license.

Assets are copied from the pinned sources in `reference/` by
`scripts/update-assets.sh` (docs/IMPLEMENTATION.md step 0.2). Never edit a copied
asset by hand; change the script and run it again.

A grammar, theme or font whose license is not permissive is not bundled. It is
recorded in `NOTICE` instead.
