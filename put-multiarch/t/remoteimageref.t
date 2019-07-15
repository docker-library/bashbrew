use Mojo::Base -strict;

use Test::More;

require_ok('Bashbrew::RemoteImageRef');

my @distributionReferenceTestcases = (
	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference_test.go#L31-L173
	{
		input =>      'test_com',
		repository => 'test_com',
	},
	{
		input =>      'test.com:tag',
		repository => 'test.com',
		tag =>        'tag',
	},
	{
		input =>      'test.com:5000',
		repository => 'test.com',
		tag =>        '5000',
	},
	{
		input =>      'test.com/repo:tag',
		domain =>     'test.com',
		repository => 'test.com/repo',
		tag =>        'tag',
	},
	{
		input =>      'test:5000/repo',
		domain =>     'test:5000',
		repository => 'test:5000/repo',
	},
	{
		input =>      'test:5000/repo:tag',
		domain =>     'test:5000',
		repository => 'test:5000/repo',
		tag =>        'tag',
	},
	{
		input =>      'test:5000/repo@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
		domain =>     'test:5000',
		repository => 'test:5000/repo',
		digest =>     'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
	},
	{
		input =>      'test:5000/repo:tag@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
		domain =>     'test:5000',
		repository => 'test:5000/repo',
		tag =>        'tag',
		digest =>     'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
	},
	{
		input =>      'test:5000/repo',
		domain =>     'test:5000',
		repository => 'test:5000/repo',
	},
	{
		input => '',
		err =>   'ErrNameEmpty',
	},
	{
		input => ':justtag',
		err =>   'ErrReferenceInvalidFormat',
	},
	{
		input => '@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
		err =>   'ErrReferenceInvalidFormat',
	},
	{
		input => 'repo@sha256:ffffffffffffffffffffffffffffffffff',
		err =>   'digest.ErrDigestInvalidLength',
	},
	{
		input => 'validname@invaliddigest:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
		err =>   'digest.ErrDigestUnsupported',
	},
	{
		input => 'Uppercase:tag',
		err =>   'ErrNameContainsUppercase',
	},
	# FIXME 'Uppercase' is incorrectly handled as a domain-name here, therefore passes.
	# See https://github.com/docker/distribution/pull/1778, and https://github.com/docker/docker/pull/20175
	#{
	#	input => 'Uppercase/lowercase:tag',
	#	err =>   ErrNameContainsUppercase,
	#},
	{
		input => 'test:5000/Uppercase/lowercase:tag',
		err =>   'ErrNameContainsUppercase',
	},
	{
		input =>      'lowercase:Uppercase',
		repository => 'lowercase',
		tag =>        'Uppercase',
	},
	{
		input => ('a/' x 128) . 'a:tag',
		err =>   'ErrNameTooLong',
	},
	{
		input =>      ('a/' x 127) . 'a:tag-puts-this-over-max',
		#domain =>     'a', # TIANON: we don't count this as the "domain" for this input due to forced "normalization"
		repository => ('a/' x 127) . 'a',
		tag =>        'tag-puts-this-over-max',
	},
	{
		input => 'aa/asdf$$^/aa',
		err =>   'ErrReferenceInvalidFormat',
	},
	{
		input =>      'sub-dom1.foo.com/bar/baz/quux',
		domain =>     'sub-dom1.foo.com',
		repository => 'sub-dom1.foo.com/bar/baz/quux',
	},
	{
		input =>      'sub-dom1.foo.com/bar/baz/quux:some-long-tag',
		domain =>     'sub-dom1.foo.com',
		repository => 'sub-dom1.foo.com/bar/baz/quux',
		tag =>        'some-long-tag',
	},
	{
		input =>      'b.gcr.io/test.example.com/my-app:test.example.com',
		domain =>     'b.gcr.io',
		repository => 'b.gcr.io/test.example.com/my-app',
		tag =>        'test.example.com',
	},
	{
		input =>      'xn--n3h.com/myimage:xn--n3h.com', # ☃.com in punycode
		domain =>     'xn--n3h.com',
		repository => 'xn--n3h.com/myimage',
		tag =>        'xn--n3h.com',
	},
	{
		input =>      'xn--7o8h.com/myimage:xn--7o8h.com@sha512:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff', # 🐳.com in punycode
		domain =>     'xn--7o8h.com',
		repository => 'xn--7o8h.com/myimage',
		tag =>        'xn--7o8h.com',
		digest =>     'sha512:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
	},
	{
		input =>      'foo_bar.com:8080',
		repository => 'foo_bar.com',
		tag =>        '8080',
	},
	{
		input =>      'foo/foo_bar.com:8080',
		#domain =>     'foo', # TIANON: we don't count this as the "domain" for this input due to forced "normalization"
		repository => 'foo/foo_bar.com',
		tag =>        '8080',
	},

	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference_test.go#L245-L268
	{
		input => '',
		err =>   'ErrNameEmpty',
	},
	{
		input => ':justtag',
		err =>   'ErrReferenceInvalidFormat',
	},
	{
		input => '@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
		err =>   'ErrReferenceInvalidFormat',
	},
	{
		input => 'validname@invaliddigest:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
		err =>   'ErrReferenceInvalidFormat',
	},
	{
		input => ('a/' x 128) . 'a:tag',
		err =>   'ErrNameTooLong',
	},
	{
		input => 'aa/asdf$$^/aa',
		err =>   'ErrReferenceInvalidFormat',
	},

	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference_test.go#L289-L318
	{
		input =>  'test.com/foo',
		domain => 'test.com',
		name =>   'foo',
	},
	{
		input =>  'test_com/foo',
		domain => '',
		name =>   'test_com/foo',
	},
	{
		input =>  'test:8080/foo',
		domain => 'test:8080',
		name =>   'foo',
	},
	{
		input =>  'test.com:8080/foo',
		domain => 'test.com:8080',
		name =>   'foo',
	},
	{
		input =>  'test-com:8080/foo',
		domain => 'test-com:8080',
		name =>   'foo',
	},
	{
		input =>  'xn--n3h.com:18080/foo',
		domain => 'xn--n3h.com:18080',
		name =>   'foo',
	},

	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference_test.go#L476-L501
	{
		name =>     'test.com/foo',
		tag =>      'tag',
		combined => 'test.com/foo:tag',
	},
	{
		name =>     'foo',
		tag =>      'tag2',
		combined => 'foo:tag2',
	},
	{
		name =>     'test.com:8000/foo',
		tag =>      'tag4',
		combined => 'test.com:8000/foo:tag4',
	},
	{
		name =>     'test.com:8000/foo',
		tag =>      'TAG5',
		combined => 'test.com:8000/foo:TAG5',
	},
	{
		name =>     'test.com:8000/foo',
		digest =>   'sha256:1234567890098765432112345667890098765',
		tag =>      'TAG5',
		combined => 'test.com:8000/foo:TAG5@sha256:1234567890098765432112345667890098765',
	},

	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference_test.go#L538-L558
	{
		name =>     'test.com/foo',
		digest =>   'sha256:1234567890098765432112345667890098765',
		combined => 'test.com/foo@sha256:1234567890098765432112345667890098765',
	},
	{
		name =>     'foo',
		digest =>   'sha256:1234567890098765432112345667890098765',
		combined => 'foo@sha256:1234567890098765432112345667890098765',
	},
	{
		name =>     'test.com:8000/foo',
		digest =>   'sha256:1234567890098765432112345667890098765',
		combined => 'test.com:8000/foo@sha256:1234567890098765432112345667890098765',
	},
	{
		name =>     'test.com:8000/foo',
		digest =>   'sha256:1234567890098765432112345667890098765',
		tag =>      'latest',
		combined => 'test.com:8000/foo:latest@sha256:1234567890098765432112345667890098765',
	},

	# TODO https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference_test.go#L594-L629
	#{
	#	input =>  'test.com/foo',
	#	domain => 'test.com',
	#	name =>   'foo',
	#},
	#{
	#	input =>  'test:8080/foo',
	#	domain => 'test:8080',
	#	name =>   'foo',
	#},
	#{
	#	input => 'test_com/foo',
	#	err =>   'ErrNameNotCanonical',
	#},
	#{
	#	input => 'test.com',
	#	err =>   'ErrNameNotCanonical',
	#},
	#{
	#	input => 'foo',
	#	err =>   'ErrNameNotCanonical',
	#},
	#{
	#	input => 'library/foo',
	#	err =>   'ErrNameNotCanonical',
	#},
	#{
	#	input =>  'docker.io/library/foo',
	#	domain => 'docker.io',
	#	name =>   'library/foo',
	#},
	## Ambiguous case, parser will add 'library/' to foo
	#{
	#	input => 'docker.io/foo',
	#	err =>   'ErrNameNotCanonical',
	#},
);
for my $testcase (@distributionReferenceTestcases) {
	my $input = $testcase->{input} // $testcase->{name};
	my ($ref, $err);
	$err = $@ unless eval { $ref = Bashbrew::RemoteImageRef->new($input); 1 };
	if ($testcase->{err}) {
		ok $err, "expected error parsing '$input' ($testcase->{err})";
	}
	elsif ($testcase->{combined}) {
		$ref->tag($testcase->{tag}) if $testcase->{tag};
		$ref->digest($testcase->{digest}) if $testcase->{digest};
		is $ref->to_string, $testcase->{combined}, "right combined string parsing $testcase->{name}";
	}
	else {
		is $err, undef, "no errors parsing '$input'";
		is $ref->host // '', $testcase->{domain} // '', "right host parsing '$input'";
		is $ref->docker_name, $testcase->{repository}, "right docker_name parsing '$input'" if $testcase->{repository};
		is $ref->repo, $testcase->{name}, "right repo parsing '$input'" if $testcase->{name};
		is $ref->tag, $testcase->{tag}, "right tag parsing '$input'";
		is $ref->digest, $testcase->{digest}, "right digest parsing '$input'";
	}
}

my @distributionNormalizeTestcases = (
	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/normalize_test.go#L134-L231
	{
		RemoteName =>    'fooo/bar',
		FamiliarName =>  'fooo/bar',
		FullName =>      'docker.io/fooo/bar',
		AmbiguousName => 'index.docker.io/fooo/bar',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'library/ubuntu',
		FamiliarName =>  'ubuntu',
		FullName =>      'docker.io/library/ubuntu',
		AmbiguousName => 'library/ubuntu',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'nonlibrary/ubuntu',
		FamiliarName =>  'nonlibrary/ubuntu',
		FullName =>      'docker.io/nonlibrary/ubuntu',
		AmbiguousName => '',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'other/library',
		FamiliarName =>  'other/library',
		FullName =>      'docker.io/other/library',
		AmbiguousName => '',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'private/moonbase',
		FamiliarName =>  '127.0.0.1:8000/private/moonbase',
		FullName =>      '127.0.0.1:8000/private/moonbase',
		AmbiguousName => '',
		Domain =>        '127.0.0.1:8000',
	},
	{
		RemoteName =>    'privatebase',
		FamiliarName =>  '127.0.0.1:8000/privatebase',
		FullName =>      '127.0.0.1:8000/privatebase',
		AmbiguousName => '',
		Domain =>        '127.0.0.1:8000',
	},
	{
		RemoteName =>    'private/moonbase',
		FamiliarName =>  'example.com/private/moonbase',
		FullName =>      'example.com/private/moonbase',
		AmbiguousName => '',
		Domain =>        'example.com',
	},
	{
		RemoteName =>    'privatebase',
		FamiliarName =>  'example.com/privatebase',
		FullName =>      'example.com/privatebase',
		AmbiguousName => '',
		Domain =>        'example.com',
	},
	{
		RemoteName =>    'private/moonbase',
		FamiliarName =>  'example.com:8000/private/moonbase',
		FullName =>      'example.com:8000/private/moonbase',
		AmbiguousName => '',
		Domain =>        'example.com:8000',
	},
	{
		RemoteName =>    'privatebasee',
		FamiliarName =>  'example.com:8000/privatebasee',
		FullName =>      'example.com:8000/privatebasee',
		AmbiguousName => '',
		Domain =>        'example.com:8000',
	},
	{
		RemoteName =>    'library/ubuntu-12.04-base',
		FamiliarName =>  'ubuntu-12.04-base',
		FullName =>      'docker.io/library/ubuntu-12.04-base',
		AmbiguousName => 'index.docker.io/library/ubuntu-12.04-base',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'library/foo',
		FamiliarName =>  'foo',
		FullName =>      'docker.io/library/foo',
		AmbiguousName => 'docker.io/foo',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'library/foo/bar',
		FamiliarName =>  'library/foo/bar',
		FullName =>      'docker.io/library/foo/bar',
		AmbiguousName => '',
		Domain =>        'docker.io',
	},
	{
		RemoteName =>    'store/foo/bar',
		FamiliarName =>  'store/foo/bar',
		FullName =>      'docker.io/store/foo/bar',
		AmbiguousName => '',
		Domain =>        'docker.io',
	},
);
for my $testcase (@distributionNormalizeTestcases) {
	for my $refString ($testcase->{FamiliarName}, $testcase->{FullName}, $testcase->{AmbiguousName}) {
		next unless $refString;
		my $ref = Bashbrew::RemoteImageRef->new($refString);
		is $ref->to_string, $testcase->{FamiliarName}, "right to_string parsing '$refString'";
		is $ref->canonical_name, $testcase->{FullName}, "right canonical_name parsing '$refString'";
		is $ref->canonical_host, $testcase->{Domain}, "right canonical_host parsing '$refString'";
		is $ref->canonical_repo, $testcase->{RemoteName}, "right canonical_repo parsing '$refString'";
	}
}

my @distributionNormalizeRefTestcases = (
	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/normalize_test.go#L633-L687
	{
		name =>     'nothing',
		input =>    'busybox',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'tag only',
		input =>    'busybox:latest',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'digest only',
		input =>    'busybox@sha256:e6693c20186f837fc393390135d8a598a96a833917917789d63766cab6c59582',
		expected => 'docker.io/library/busybox@sha256:e6693c20186f837fc393390135d8a598a96a833917917789d63766cab6c59582',
	},
	{
		name =>     'path only',
		input =>    'library/busybox',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'hostname only',
		input =>    'docker.io/busybox',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'no tag',
		input =>    'docker.io/library/busybox',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'no path',
		input =>    'docker.io/busybox:latest',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'no hostname',
		input =>    'library/busybox:latest',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'full reference with tag',
		input =>    'docker.io/library/busybox:latest',
		expected => 'docker.io/library/busybox:latest',
	},
	{
		name =>     'gcr reference without tag',
		input =>    'gcr.io/library/busybox',
		expected => 'gcr.io/library/busybox:latest',
	},
	{
		name =>     'both tag and digest',
		input =>    'gcr.io/library/busybox:latest@sha256:e6693c20186f837fc393390135d8a598a96a833917917789d63766cab6c59582',
		expected => 'gcr.io/library/busybox@sha256:e6693c20186f837fc393390135d8a598a96a833917917789d63766cab6c59582',
	},
);
for my $testcase (@distributionNormalizeRefTestcases) {
	my $ref = Bashbrew::RemoteImageRef->new($testcase->{input});
	is $ref->to_canonical_string, $testcase->{expected}, "right to_canonical_string for '$testcase->{name}' parsing '$testcase->{input}'";
}

# TODO consider scraping https://github.com/containerd/containerd/blob/7acdb16882080edbe939997e8ed09d7ef3a02cc6/reference/reference_test.go too

# test/verify "clone"
my $input = 'reg1.example.com/foo/bar:baz';
my $orig = Bashbrew::RemoteImageRef->new($input);
is $orig->to_string, $input;
my $clone = $orig->clone->host('reg2.example.com');
is $orig->to_string, $input;
isnt $clone->to_string, $input;
$clone->host('reg1.example.com');
is $clone->to_string, $input;
# test/verify "registry_host"
is $clone->registry_host, 'reg1.example.com';
$clone->host(undef);
is $clone->registry_host, 'registry-1.docker.io';
$clone->host('docker.io');
is $clone->registry_host, 'registry-1.docker.io';
$clone->host('reg1.example.com');
is $clone->registry_host, 'reg1.example.com';
# "repo_name", "repo_org"
is $orig->repo_name, 'bar';
is $orig->repo_org, 'foo';
$clone->repo('foo/bar/baz');
is $clone->repo_name, 'baz';
is $clone->repo_org, 'foo/bar';
# "obj"
is $clone->obj, 'baz';
$clone->digest('sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff');
is $clone->obj, 'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff';
$clone->tag(undef);
is $clone->obj, 'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff';
$clone->digest(undef);
is $clone->obj, 'latest';

done_testing();
