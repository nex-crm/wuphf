# Release Day Troubleshooting

## Published Release Already Exists

The publish job may update draft releases, but it refuses to replace assets on a
published release. If a published release already exists for the tag, either
delete that release manually before rerunning the workflow or bump the tag and
ship a new release.

This guard prevents live clients from fetching an old `latest*.yml` and then
downloading newly clobbered artifact bytes with a different sha512.
