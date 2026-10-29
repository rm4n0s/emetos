FROM node:lts-alpine
ENV NODE_ENV=production
WORKDIR /usr/src/app
COPY ["package.json", "package-lock.json*", "./"]
RUN npm install --production --silent
COPY . .
EXPOSE 3000
USER node
CMD ["npm", "start"]
