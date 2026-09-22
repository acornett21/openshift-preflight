## UBI EOL Plan
The below steps are based around the below Jira comment and the need for a possible EOL check. (data quality is tbd at this point, so we are just exploring the posibility and will test against real data later)
1. [jira](https://redhat.atlassian.net/browse/EDPP-406?focusedCommentId=18283475)


# Plan
1. Determine what it takes to add the below pyxis query. Since these needs to be exported to the check layer, use `CertifiedImagesContainingLayers` in  /home/acornett/go/src/github.com/acornett21/openshift-preflight/internal/pyxis/layers.go as an example
2. Determine what it takes to create a new check, name for now can be `IsUBIEOLCheck`
3. Determine how to add this to the default container policy, and every other container policy minus `Konflux`
4. Anything else that is needed and not defined in the above