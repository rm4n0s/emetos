Solutions of the C# pack (step 24b.7).

solution/   the solution of the real-server tests (build tag emetos_csharpls): App.sln with one SDK-style project that has no package
            references, so that restoring it needs no network (the .NET SDK's own reference packs are enough). The tests copy it into a
            temporary folder; nothing here is built. Its NuGet.config has no package source at all, so a restore
            that wanted a package would fail instead of reaching for nuget.org.
