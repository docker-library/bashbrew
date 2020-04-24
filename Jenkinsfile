properties([
	buildDiscarder(logRotator(daysToKeepStr: '14')),
	disableConcurrentBuilds(),
	pipelineTriggers([
		cron('H H * * *'),
	]),
])

node {
	dir('bashbrew') {
		stage('Checkout') {
			checkout scm
		}

		ansiColor('xterm') {
			stage('Build') {
				sh '''
					docker build -t "bashbrew:$BRANCH_NAME" --pull -f Dockerfile.release .
					rm -rf bin
					docker run -i --rm "bashbrew:$BRANCH_NAME" tar -c bin | tar -xv
				'''
			}
		}

		dir('bin') {
			stage('Archive') {
				archiveArtifacts(
					artifacts: '**',
					fingerprint: true,
					onlyIfSuccessful: true,
				)
			}
		}
	}
}
