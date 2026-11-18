FROM mcr.microsoft.com/playwright@sha256:9bd26ad900bb5e0f4dee75839e957a89ae89c2b7ab1e76050e559790e946b948
WORKDIR /acceptance
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY playwright.config.js smoke.spec.js ./
USER pwuser
CMD ["./node_modules/.bin/playwright", "test"]
