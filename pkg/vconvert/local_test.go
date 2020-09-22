package vconvert

// for these tests to run docker needs to be installed
// and the hello-world image needs to be pulled by docker and containerd
// docker pull hello-world
// ctr images pull docker.io/library/hello-world:latest
// the user needs to be in the docker and root group
// sudo usermod -a -G root,docker USER

// func TestDownloadInformationDocker(t *testing.T) {

// 	r, _ := NewContainerConverter("local.docker/hello-world", "", nil)

// 	err := r.downloadInformationDocker("", "")
// 	assert.Error(t, err)

// 	err = r.downloadInformationDocker("hello-world", "")
// 	assert.Error(t, err)

// 	err = r.downloadInformationDocker("hello-world", "latest")
// 	if !assert.NoError(t, err) || !assert.Equal(t, 1, len(r.layers)) {
// 		t.Fatal("Could not get docker info")
// 	}

// 	err = r.downloadBlobs("")
// 	assert.Error(t, err)

// 	dir, _ := ioutil.TempDir("", "vtest")
// 	err = r.downloadBlobs(dir)
// 	assert.NoError(t, err)
// }

// func TestDownloadInformationContainerd(t *testing.T) {

// 	r, _ := NewContainerConverter("local.containerd/docker.io/library/hello-world:latest", "", nil)

// 	err := r.downloadInformationContainerd("", "")
// 	assert.Error(t, err)

// 	err = r.downloadInformationContainerd("hello-world", "")
// 	assert.Error(t, err)

// 	err = r.downloadInformationContainerd("docker.io/library/hello-world", "latest")
// 	if !assert.NoError(t, err) || !assert.Equal(t, 1, len(r.layers)) {
// 		t.Fatal("Could not get docker info")
// 	}

// 	err = r.downloadBlobs("")
// 	assert.Error(t, err)

// 	dir, _ := ioutil.TempDir("", "vtest")
// 	err = r.downloadBlobs(dir)
// 	assert.NoError(t, err)

// }
