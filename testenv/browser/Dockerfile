FROM mcr.microsoft.com/playwright:v1.56.1-noble

WORKDIR /tests
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY console.mjs ./

CMD ["node", "console.mjs"]
