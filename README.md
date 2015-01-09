gitpacklib
==========

gitpacklib is an ***experimental*** library that facilitates creating an SSH-based git server that receives pushes from git clients and saves git data to an arbitrary storage medium (not just a filesystem ```.git``` directory). Rather than wrapping the ```git-receive-pack``` command line utility, the git object unpacking code is implemented natively in Go. Similarly, an SSH server is included that is based on ```golang.org/x/crypto/ssh```, so an external SSH daemon is not required.

The current implementation is not designed for efficiency, but for simplicity. The unpacking is done as the pack file is received so large repositories will use a lot of storage space in the backing store. This may change in a future version, where the unpacking can be done on the fly at usage time similar to ```git``` itself.

gitpacklib does not include main binary, though the examples provide basic usage with dummy setup, authentication and storage backends. A typical project would fork these examples to implement custom logic for the specific use case.

