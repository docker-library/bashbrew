#input package 
# {
#     name: "packageName",
#     version: "packageVersion",
#     params: {
#         "foo": "bar"
#     }
#     licenses: ["packageLicense" ... ]
# }
#output: object
def sbom:
    {
		spdxVersion: "SPDX-2.3",
		SPDXID: "SPDXRef-DOCUMENT",
		name: (.name + "-sbom"),
		packages: [
			{
				name: .name,
				versionInfo: .version,
				SPDXID: ("SPDXRef-Package--" + .name),
				externalRefs: [
					{
						referenceCategory: "PACKAGE-MANAGER",
						referenceType: "purl",
						referenceLocator: ("pkg:generic/" + .name + "@" + .version + "?" + (.params | [to_entries[] | .key + "=" + .value] | join("\u0026")))
					}
				],
				licenseDeclared: (if .licenses | length > 0 then
					(.licenses | join(" AND "))
				else
					"NOASSERTION"
				end)
			}
		]
	}
;
