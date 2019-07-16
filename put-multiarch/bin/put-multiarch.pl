#!/usr/bin/env perl
use Mojo::Base -strict, -signatures;

# this is a replacement for "bashbrew put-shared" (without "--single-arch") to combine many architecture-specific repositories into manifest lists in a separate repository
# for example, combining amd64/bash:latest, arm32v5/bash:latest, ..., s390x/bash:latest into a single library/bash:latest manifest list
# (in a more efficient way than manifest-tool can do generically such that we can reasonably do 3700+ no-op tag pushes individually in ~9 minutes)

use Digest::SHA;
use Getopt::Long;
use Mojo::Promise;
use Mojo::UserAgent;
use Mojo::Util;

use Bashbrew qw( arch_to_platform bashbrew );
use Bashbrew::RemoteImageRef;
use Bashbrew::RegistryUserAgent;

my $ua = Bashbrew::RegistryUserAgent->new;

my $dryRun = '';
my $insecureRegistry = '';
GetOptions(
	'dry-run!' => \$dryRun,
	'insecure!' => \$insecureRegistry,
) or die "error in command line arguments\n";

$ua->insecure($insecureRegistry);

# TODO make this "die" conditional based on whether we're actually targeting Docker Hub?
$ua->hubProxy($ENV{DOCKERHUB_PUBLIC_PROXY} || die 'missing DOCKERHUB_PUBLIC_PROXY env (https://github.com/tianon/dockerhub-public-proxy)');

# get list of manifest list items and necessary blobs for a particular architecture
sub get_arch_p ($targetRef, $arch, $archRef) {
	return $ua->get_manifest_p($archRef)->then(sub ($manifestData = undef) {
		return unless $manifestData;
		my ($digest, $manifest, $size) = ($manifestData->{digest}, $manifestData->{manifest}, $manifestData->{size});

		my $mediaType = $manifestData->{mediaType};
		if ($mediaType eq 'application/vnd.docker.distribution.manifest.list.v2+json') {
			# jackpot -- if it's already a manifest list, the hard work is done!
			return ($archRef, $manifest->{manifests});
		}
		if ($mediaType eq 'application/vnd.docker.distribution.manifest.v1+json' || $mediaType eq 'application/vnd.docker.distribution.manifest.v2+json') {
			my $manifestListItem = {
				mediaType => $mediaType,
				size => $size,
				digest => $digest,
				platform => {
					arch_to_platform($arch),
					($manifest->{'os.version'} ? ('os.version' => $manifest->{'os.version'}) : ()),
				},
			};
			if ($manifestListItem->{platform}{os} eq 'windows' && !$manifestListItem->{platform}{'os.version'} && $mediaType eq 'application/vnd.docker.distribution.manifest.v2+json') {
				# if we're on Windows, we need to make an effort to fetch the "os.version" value from the config for the platform object
				return get_blob_p($archRef->clone->digest($manifest->{config}{digest}))->then(sub ($config = undef) {
					if ($config && $config->{'os.version'}) {
						$manifestListItem->{platform}{'os.version'} = $config->{'os.version'};
					}
					return ($archRef, [ $manifestListItem ]);
				});
			}
			else {
				return ($archRef, [ $manifestListItem ]);
			}
		}

		die "unknown mediaType '$mediaType' for '$archRef'";
	});
}

sub needed_artifacts_p ($targetRef, $sourceRef) {
	return $ua->head_manifest_p($targetRef->clone->digest($sourceRef->digest))->then(sub ($exists) {
		return if $exists;

		return $ua->get_manifest_p($sourceRef)->then(sub ($manifestData = undef) {
			return unless $manifestData;

			my $manifest = $manifestData->{manifest};
			my $schemaVersion = $manifest->{schemaVersion};
			my @blobs;
			if ($schemaVersion == 1) {
				push @blobs, map { $_->{blobSum} } @{ $manifest->{fsLayers} };
			}
			elsif ($schemaVersion == 2) {
				die "this should never happen: $manifest->{mediaType}" unless $manifest->{mediaType} eq 'application/vnd.docker.distribution.manifest.v2+json'; # sanity check
				push @blobs, $manifest->{config}{digest}, map { $_->{urls} ? () : $_->{digest} } @{ $manifest->{layers} };
			}
			else {
				die "this should never happen: $schemaVersion"; # sanity check
			}

			return Mojo::Promise->all(
				Mojo::Promise->resolve([ 'manifest', $sourceRef ]),
				(
					@blobs ? Mojo::Promise->map({ concurrency => 3 }, sub ($blob) {
						return $ua->head_blob_p($targetRef->clone->digest($blob))->then(sub ($exists) {
							return if $exists;
							return 'blob', $sourceRef->clone->digest($blob);
						});
					}, @blobs) : (),
				),
			)->then(sub { map { @$_ } @_ });
		});
	});
}

Mojo::Promise->map({ concurrency => 8 }, sub ($img) {
	die "image '$img' is missing explict namespace -- bailing to avoid accidental push to 'library'" unless $img =~ m!/!;

	my $ref = Bashbrew::RemoteImageRef->new($img);

	my @refs = (
		$ref->tag
		? ( $ref )
		: (
			map { $ref->clone->tag((split /:/)[1]) }
			List::Util::uniq sort
			split /\n/, bashbrew('list', $ref->repo_name)
		)
	);
	return Mojo::Promise->resolve unless @refs; # no tags, nothing to do! (opensuse, etc)

	return Mojo::Promise->map({ concurrency => 1 }, sub ($ref) {
		my @arches = (
			List::Util::uniq sort
			split /\n/, bashbrew('cat', '--format', '{{ range .Entries }}{{ range .Architectures }}{{ . }}={{ archNamespace . }}{{ "\n" }}{{ end }}{{ end }}', $ref->repo_name . ':' . $ref->tag)
		);
		return Mojo::Promise->resolve unless @arches; # no arches, nothing to do!

		return Mojo::Promise->map({ concurrency => 1 }, sub ($archData) {
			my ($arch, $archNamespace) = split /=/, $archData;
			my $archRef = Bashbrew::RemoteImageRef->new($archNamespace . '/' . $ref->repo_name . ':' . $ref->tag);
			die "'$archRef' registry does not match '$ref' registry" unless $archRef->registry_host eq $ref->registry_host;
			return get_arch_p($ref, $arch, $archRef);
		}, @arches)->then(sub (@archResponses) {
			my @manifestListItems;
			my @neededArtifactPromises;
			for my $archResponse (@archResponses) {
				next unless @$archResponse;
				my ($archRef, $manifestListItems) = @$archResponse;
				push @manifestListItems, @$manifestListItems;
				push @neededArtifactPromises, map { my $digest = $_->{digest}; sub { needed_artifacts_p($ref, $archRef->clone->digest($digest)) } } @$manifestListItems;
			}

			my $manifestList = {
				schemaVersion => 2,
				mediaType => 'application/vnd.docker.distribution.manifest.list.v2+json',
				manifests => \@manifestListItems,
			};
			my $manifestListJson = Mojo::JSON::encode_json($manifestList);
			my $manifestListDigest = 'sha256:' . Digest::SHA::sha256_hex($manifestListJson);

			return $ua->head_manifest_p($ref->clone->digest($manifestListDigest))->then(sub ($exists) {
				# if we already have the manifest we're planning to push in the namespace where we plan to push it, we can skip all blob mounts! \m/
				return if $exists;
				# (we can also skip if we're in "dry run" mode since we only care about the final manifest matching in that case)
				return if $dryRun;

				return (
					@neededArtifactPromises
					? Mojo::Promise->map({ concurrency => 1 }, sub { $_->() }, @neededArtifactPromises)
					: Mojo::Promise->resolve
				)->then(sub (@neededArtifacts) {
					@neededArtifacts = map { @$_ } @neededArtifacts;
					# now "@neededArtifacts" is a list of tuples of the format [ sourceNamespace, sourceRepo, type, digest ], ready for cross-repo mounting / PUTing (where type is "blob" or "manifest")
					my @mountBlobPromises;
					my @putManifestPromises;
					for my $neededArtifact (@neededArtifacts) {
						next unless @$neededArtifact;
						my ($type, $artifactRef) = @$neededArtifact;
						if ($type eq 'blob') {
							# https://docs.docker.com/registry/spec/api/#mount-blob
							push @mountBlobPromises, sub { $ua->authenticated_registry_req_p(
								POST => $ref,
								'repository:' . $ref->repo . ':push repository:' . $artifactRef->repo . ':pull',
								'blobs/uploads/?mount=' . $artifactRef->digest . '&from=' . $artifactRef->repo,
							) };
						}
						elsif ($type eq 'manifest') {
							push @putManifestPromises, sub { $ua->get_manifest_p($artifactRef)->then(sub ($manifestData = undef) {
								return unless $manifestData;
								return $ua->authenticated_registry_req_p(
									PUT => $ref,
									'repository:' . $ref->repo . ':push',
									'manifests/' . $artifactRef->digest,
									$manifestData->{mediaType}, $manifestData->{verbatim},
								)->then(sub ($tx) {
									if (my $err = $tx->error) {
										die "Failed to PUT $artifactRef to $ref: " . $err->{message};
									}
									return;
								});
							}) };
						}
						else {
							die "this should never happen: $type"; # sanity check
						}
					}

					# mount any necessary blobs
					return (
						@mountBlobPromises
						? Mojo::Promise->map({ concurrency => 1 }, sub { $_->() }, @mountBlobPromises)
						: Mojo::Promise->resolve
					)->then(sub {
						# ... *then* push any missing image manifests (because they'll fail to push if the blobs aren't pushed first)
						if (@putManifestPromises) {
							return Mojo::Promise->map({ concurrency => 1 }, sub { $_->() }, @putManifestPromises);
						}
						return;
					});
				});
			})->then(sub {
				# let's do one final check of the tag we're pushing to see if it's already the manifest we expect it to be (to avoid making literally every image constantly "Updated a few seconds ago" all the time)
				return $ua->get_manifest_p($ref)->then(sub ($manifestData = undef) {
					if ($manifestData && $manifestData->{digest} eq $manifestListDigest) {
						say "Skipping $ref ($manifestListDigest)" unless $dryRun; # if we're in "dry run" mode, we need clean output
						return;
					}

					if ($dryRun) {
						say "Would push $ref ($manifestListDigest)";
						return;
					}

					# finally, all necessary blobs and manifests are pushed, we've verified that we do in fact need to push this manifest, so we should be golden to push it!
					return $ua->authenticated_registry_req_p(
						PUT => $ref,
						$ref->repo . ':push',
						'manifests/' . $ref->tag,
						$manifestList->{mediaType}, $manifestListJson
					)->then(sub ($tx) {
						if (my $err = $tx->error) {
							die 'Failed to push manifest list: ' . $err->{message};
						}
						my $digest = $tx->res->headers->header('Docker-Content-Digest');
						say "Pushed $ref ($digest)";
						say {*STDERR} "WARNING: expected '$manifestListDigest', got '$digest' (for '$ref')" unless $manifestListDigest eq $digest;
					});
				});
			});
		});
	}, @refs);
}, @ARGV)->catch(sub {
	say {*STDERR} "ERROR: $_" for @_;
	exit scalar @_;
})->wait;
