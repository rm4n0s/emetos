FROM mcr.microsoft.com/dotnet/aspnet:3.1 AS base
WORKDIR /app
EXPOSE 80

ENV ASPNETCORE_URLS=http://+:80

FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:3.1 AS build
ARG configuration=Release
WORKDIR /src
COPY ["old/Old.csproj", "old/"]
RUN dotnet restore "old/Old.csproj"
COPY . .
WORKDIR "/src/old"
RUN dotnet build "Old.csproj" -c $configuration -o /app/build

FROM build AS publish
ARG configuration=Release
RUN dotnet publish "Old.csproj" -c $configuration -o /app/publish /p:UseAppHost=false

FROM base AS final
WORKDIR /app
COPY --from=publish /app/publish .
ENTRYPOINT ["dotnet", "Old.dll"]
