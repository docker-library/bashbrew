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

use Bashbrew::RemoteImageRef;

my $dryRun = '';
my $insecureRegistry = '';
GetOptions(
	'dry-run!' => \$dryRun,
	'insecure!' => \$insecureRegistry,
) or die "error in command line arguments\n";

my $publicProxy = $ENV{DOCKERHUB_PUBLIC_PROXY} || die 'missing DOCKERHUB_PUBLIC_PROXY env (https://github.com/tianon/dockerhub-public-proxy)';

my $registryScheme = ($insecureRegistry ? 'http' : 'https');

my $ua = Mojo::UserAgent->new->max_redirects(10)->connect_timeout(120)->inactivity_timeout(120);
$ua->transactor->name(join ' ',
# https://github.com/docker/docker/blob/v1.11.2/dockerversion/useragent.go#L13-L34
	'docker/1.11.2',
	'go/1.6.2',
	'git-commit/v1.11.2',
	'kernel/4.4.11',
	'os/linux',
	'arch/amd64',
	# BOGUS USER AGENTS FOR THE BOGUS USER AGENT THRONE
);

# this is normally handled by DOCKERHUB_PUBLIC_PROXY but is necessary for alternative registries
my $acceptHeader = [
	'application/vnd.docker.distribution.manifest.list.v2+json',
	'application/vnd.docker.distribution.manifest.v2+json',
	# TODO OCI media types?
];

my $simpleRetries = 10;
sub ua_retry_simple_req_p ($tries, $method, @args) {
	--$tries;
	my $lastTry = $tries < 1;

	my $methodP = lc($method) . '_p';
	my $prom = $ua->$methodP(@args);
	if (!$lastTry) {
		$prom = $prom->then(sub ($tx) {
			return $tx if !$tx->error || $tx->res->code == 404 || $tx->res->code == 401;
			return ua_retry_simple_req_p($tries, $method, @args);
		});
	}
	return $prom;
}

sub arch_to_platform ($arch) {
	if ($arch =~ m{
		^
		(?: ([^-]+) - )? # optional "os" prefix ("windows-", etc)
		([^-]+?) # "architecture" bit ("arm64", "s390x", etc)
		(v[0-9]+)? # optional "variant" suffix ("v7", "v6", etc)
		$
	}x) {
		return (
			os => $1 // 'linux',
			architecture => (
				$2 eq 'i386'
				? '386'
				: (
					$2 eq 'arm32'
					? 'arm'
					: $2
				)
			),
			($3 ? (variant => $3) : ()),
		);
	}
	die "unrecognized architecture format in: $arch";
}

# TODO make this promise-based and non-blocking?
# https://github.com/jberger/Mojolicious-Plugin-TailLog/blob/master/lib/Mojolicious/Plugin/TailLog.pm#L16-L22
# https://metacpan.org/pod/Capture::Tiny
# https://metacpan.org/pod/Mojo::IOLoop#subprocess
# https://metacpan.org/pod/IO::Async::Process
# (likely not worth it, given how quickly it typically completes)
sub bashbrew (@) {
	open my $fh, '-|', 'bashbrew', @_ or die "failed to run 'bashbrew': $!";
	local $/;
	my $output = <$fh>;
	close $fh or die "failed to close 'bashbrew'";
	chomp $output;
	return $output;
}

sub _ref_repo_url ($ref) {
	return (
		$ref->docker_host
		? $registryScheme . '://' . $ref->registry_host
		: $publicProxy
	) . '/v2/' . $ref->repo;
}

sub get_manifest_p ($ref, $tries = 10) {
	--$tries;
	my $lastTry = $tries < 1;

	state %cache;
	if ($ref->digest && $cache{$ref->digest}) {
		return Mojo::Promise->resolve($cache{$ref->digest});
	}

	return ua_retry_simple_req_p($simpleRetries, GET => _ref_repo_url($ref) . '/manifests/' . $ref->obj, { Accept => $acceptHeader })->then(sub ($tx) {
		return if $tx->res->code == 404 || $tx->res->code == 401;

		if (!$lastTry && $tx->res->code != 200) {
			return get_manifest_p($ref, $tries);
		}
		die "unexpected exit code fetching '$ref': " . $tx->res->code unless $tx->res->code == 200;

		my $digest = $tx->res->headers->header('docker-content-digest') or die "'$ref' is missing 'docker-content-digest' header";
		die "malformed 'docker-content-digest' header in '$ref': '$digest'" unless $digest =~ m!^sha256:!; # TODO steal Bashbrew::RemoteImageRef validation?

		my $manifest = $tx->res->json or die "'$ref' has bad or missing JSON";
		my $size = int($tx->res->headers->content_length);
		my $verbatim = $tx->res->body;

		return $cache{$digest} = {
			digest => $digest,
			manifest => $manifest,
			size => $size,
			verbatim => $verbatim,

			mediaType => (
				$manifest->{schemaVersion} == 1
				? 'application/vnd.docker.distribution.manifest.v1+json'
				: (
					$manifest->{schemaVersion} == 2
					? $manifest->{mediaType}
					: die "unknown schemaVersion for '$ref'"
				)
			),
		};
	});
}

sub get_blob_p ($ref, $tries = 10) {
	die "missing blob digest for '$ref'" unless $ref->digest;

	--$tries;
	my $lastTry = $tries < 1;

	state %cache;
	return Mojo::Promise->resolve($cache{$ref->digest}) if $cache{$ref->digest};

	return ua_retry_simple_req_p($simpleRetries, GET => _ref_repo_url($ref) . '/blobs/' . $ref->digest)->then(sub ($tx) {
		return if $tx->res->code == 404;

		if (!$lastTry && $tx->res->code != 200) {
			return get_blob_p($ref, $tries);
		}
		die "unexpected exit code fetching blob from '$ref'': " . $tx->res->code unless $tx->res->code == 200;

		return $cache{$ref->digest} = $tx->res->json;
	});
}

sub head_manifest_p ($ref) {
	die "missing manifest digest for HEAD '$ref'" unless $ref->digest;

	my $cacheKey = $ref->to_canonical_string;
	state %cache;
	return Mojo::Promise->resolve($cache{$cacheKey}) if $cache{$cacheKey};

	return ua_retry_simple_req_p($simpleRetries, HEAD => _ref_repo_url($ref) . '/manifests/' . $ref->digest, { Accept => $acceptHeader })->then(sub ($tx) {
		return 0 if $tx->res->code == 404 || $tx->res->code == 401;
		die "unexpected exit code HEADing manifest '$ref': " . $tx->res->code unless $tx->res->code == 200;
		return $cache{$cacheKey} = 1;
	});
}

sub head_blob_p ($ref) {
	die "missing blob digest for HEAD '$ref'" unless $ref->digest;

	my $cacheKey = $ref->to_canonical_string;
	state %cache;
	return Mojo::Promise->resolve($cache{$cacheKey}) if $cache{$cacheKey};

	return ua_retry_simple_req_p($simpleRetries, HEAD => _ref_repo_url($ref) . '/blobs/' . $ref->digest)->then(sub ($tx) {
		return 0 if $tx->res->code == 404 || $tx->res->code == 401;
		die "unexpected exit code HEADing blob '$ref': " . $tx->res->code unless $tx->res->code == 200;
		return $cache{$cacheKey} = 1;
	});
}

# get list of manifest list items and necessary blobs for a particular architecture
sub get_arch_p ($targetRef, $arch, $archRef) {
	return get_manifest_p($archRef)->then(sub ($manifestData = undef) {
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
	return head_manifest_p($targetRef->clone->digest($sourceRef->digest))->then(sub ($exists) {
		return if $exists;

		return get_manifest_p($sourceRef)->then(sub ($manifestData = undef) {
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
				Mojo::Promise->map({ concurrency => 3 }, sub ($blob) {
					return head_blob_p($targetRef->clone->digest($blob))->then(sub ($exists) {
						return if $exists;
						return 'blob', $sourceRef->clone->digest($blob);
					});
				}, @blobs),
			)->then(sub { map { @$_ } @_ });
		});
	});
}

sub get_dockerhub_creds ($ref) {
	die 'missing DOCKER_CONFIG or HOME environment variable' unless $ENV{DOCKER_CONFIG} or $ENV{HOME};

	my $config = Mojo::File->new(($ENV{DOCKER_CONFIG} || ($ENV{HOME} . '/.docker')) . '/config.json')->slurp;
	die 'missing or empty ".docker/config.json" file' unless $config;

	my $json = Mojo::JSON::decode_json($config);
	die 'invalid ".docker/config.json" file' unless $json && $json->{auths};

	my @registryHosts = ( $ref->registry_host );
	push @registryHosts, 'index.docker.io', 'docker.io' if !$ref->docker_host; # https://github.com/moby/moby/blob/fc01c2b481097a6057bec3cd1ab2d7b4488c50c4/registry/config.go#L397-L404

	for my $registry (keys %{ $json->{auths} }) {
		next unless $json->{auths}{$registry};

		my $auth = $json->{auths}{$registry}{auth};
		next unless $auth;

		# https://github.com/moby/moby/blob/34b56728ed7101c6b3cc0405f5fd6351073a8253/registry/auth.go#L202-L235
		$registry =~ s! ^ https?:// | / .+ $ !!gx;

		for my $registryHost (@registryHosts) {
			if ($registry eq $registryHost) {
				$auth = Mojo::Util::b64_decode($auth);
				return $auth if $auth && $auth =~ m!:!;
			}
		}
	}

	die 'failed to find credentials for "' . $ref->canonical_host . '" in ".docker/config.json" file';
}

sub authenticated_registry_req_p ($method, $ref, $scope, $url, $contentType = undef, $payload = undef, $tries = 10) {
	--$tries;
	my $lastTry = $tries < 1;

	my %headers = ($contentType ? ('Content-Type' => $contentType) : ());

	state %tokens;
	if (my $token = $tokens{$scope}) {
		$headers{Authorization} = "Bearer $token";
	}

	my $methodP = lc($method) . '_p';
	my $fullUrl = $registryScheme . '://' . $ref->registry_host . '/v2/' . $url;
	my $prom = $ua->$methodP($fullUrl, \%headers, ($payload ? $payload : ()));
	if (!$lastTry) {
		$prom = $prom->then(sub ($tx) {
			if (!$lastTry && $tx->res->code == 401) {
				# "Unauthorized" -- we must need to go fetch a token for this registry request (so let's go do that, then retry the original registry request)
				my $auth = $tx->res->headers->www_authenticate;
				die "unexpected WWW-Authenticate header ('$url'): $auth" unless $auth =~ m{ ^ Bearer \s+ (\S.*) $ }x;
				my $realm = $1;
				my $authUrl = Mojo::URL->new;
				while ($realm =~ m{
					# key="val",
					([^=]+)
					=
					"([^"]+)"
					,?
				}xg) {
					my ($key, $val) = ($1, $2);
					next if $key eq 'error' and $val eq 'invalid_token'; # just ignore the error if it's "invalid_token" because it likely means our token expired mid-push so we just need to renew
					die "WWW-Authenticate header error ('$url'): $val ($auth)" if $key eq 'error';
					if ($key eq 'realm') {
						$authUrl->base(Mojo::URL->new($val));
					}
					else {
						$authUrl->query->append($key => $_) for split / /, $val; # Docker's auth server expects "scope=xxx&scope=yyy" instead of "scope=xxx%20yyy"
					}
				}
				$authUrl = $authUrl->to_abs;
				say {*STDERR} "Note: grabbing auth token from $authUrl (for $fullUrl; $tries tries remain)";
				my $dockerhubCreds = get_dockerhub_creds($ref);
				return ua_retry_simple_req_p($simpleRetries, GET => $authUrl->userinfo($dockerhubCreds)->to_unsafe_string)->then(sub ($tx) {
					if (my $error = $tx->error) {
						die "registry authentication error ('$url'): " . ($error->{code} ? $error->{code} . ' -- ' : '') . $error->{message};
					}

					$tokens{$scope} = $tx->res->json->{token};
					return authenticated_registry_req_p($method, $ref, $scope, $url, $contentType, $payload, $tries);
				});
			}

			if (!$lastTry && $tx->res->code != 200) {
				return authenticated_registry_req_p($method, $ref, $scope, $url, $contentType, $payload, $tries);
			}

			if (my $error = $tx->error) {
				$tx->req->headers->authorization('REDATCTED') if $tx->req->headers->authorization;
				die "registry request error ('$url'): " . ($error->{code} ? $error->{code} . ' -- ' : '') . $error->{message} . "\n\nREQUEST:\n" . $tx->req->headers->to_string . "\n\n" . $tx->req->body . "\n\nRESPONSE:\n" . $tx->res->to_string . "\n";
			}

			return $tx;
		});
	}
	return $prom;
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

			return head_manifest_p($ref->clone->digest($manifestListDigest))->then(sub ($exists) {
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
							push @mountBlobPromises, sub { authenticated_registry_req_p(
								POST => $ref,
								'repository:' . $ref->repo . ':push repository:' . $artifactRef->repo . ':pull',
								$ref->repo . '/blobs/uploads/?mount=' . $artifactRef->digest . '&from=' . $artifactRef->repo,
							) };
						}
						elsif ($type eq 'manifest') {
							push @putManifestPromises, sub { get_manifest_p($artifactRef)->then(sub ($manifestData = undef) {
								return unless $manifestData;
								return authenticated_registry_req_p(
									PUT => $ref,
									'repository:' . $ref->repo . ':push',
									$ref->repo . '/manifests/' . $artifactRef->digest,
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
				return get_manifest_p($ref)->then(sub ($manifestData = undef) {
					if ($manifestData && $manifestData->{digest} eq $manifestListDigest) {
						say "Skipping $ref ($manifestListDigest)" unless $dryRun; # if we're in "dry run" mode, we need clean output
						return;
					}

					if ($dryRun) {
						say "Would push $ref ($manifestListDigest)";
						return;
					}

					# finally, all necessary blobs and manifests are pushed, we've verified that we do in fact need to push this manifest, so we should be golden to push it!
					return authenticated_registry_req_p(
						PUT => $ref,
						$ref->repo . ':push',
						$ref->repo . '/manifests/' . $ref->tag,
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
