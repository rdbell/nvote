# Nvote

Nvote is a decentralized, vote-driven community similar to services like Reddit and HackerNews. Nvote is powered by [nostr](https://github.com/fiatjaf/nostr).

Nvote is a work-in-progress and needs contributors.

Visit our Telegram group at [https://t.me/nvote_app](https://t.me/nvote_app).

## Using Nvote

### Run a client locally (recommended)

```
docker run --rm -it -p 1323:1323 --name nvote \
  -e NV_ENVIRONMENT='local' \
  rdbell/nvote:latest
```

Visit http://localhost:1323 in your browser.

### Access through a gateway

You can use Nvote through my public gateway at [https://nvote.co](https://nvote.co).

## Why should I want to use this instead of a centralized service like Reddit?
- It's lightweight. No ads, no javascript. No images except in posts. (inline images can be disabled in settings)
- Full feature compatibility with privacy browsers like TorBrowser with javascript disabled.
- Community-based spam prevention with no centralized moderators.
- Publicly available activity data to help the community identify vote manipulation and astroturfing.
- Anyone can host a nostr relay and mirror the data.
- Relays and clients can be run locally or be made public for other people to share.
- Ability to disable spam filters or even modify the client with custom filters.
- You don't have to rely on a single relay for content. You can configure the client to use other relays as data providers.
- Backend data is owned by nobody and can be digested by alternative clients without the need for special API permissions.

See the Nvote's about page for more info: [https://nvote.co/about](https://nvote.co/about)

## Set up a relay

View nostr relay options here: [https://github.com/fiatjaf/nostr](https://github.com/fiatjaf/nostr)

## Contributing

1. Fork the project.
2. Create your feature branch: `git checkout -b my-new-feature`
3. Commit your changes: `git commit -am 'Add some feature'`
4. Push to the branch: `git push origin my-new-feature`
5. Submit a pull request

## License

[https://opensource.org/licenses/MIT](https://opensource.org/licenses/MIT)
