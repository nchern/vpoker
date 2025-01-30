
const COVER = 'cover';
const FACE = 'face';

const suits = ['♠', '♥', '♦', '♣']; // Spades, Hearts, Diamonds, Clubs
const ranks = ['A', '2', '3', '4', '5', '6', '7', '8', '9', '10', 'J', 'Q', 'K'];

const BUTTON_LEFT = 0;

let players = {};

function getSession() {
    const cookies = document.cookie.split('; ');
    const sessionCookie = cookies.find(cookie => cookie.startsWith('session='));
    if (sessionCookie) {
        const encodedValue = sessionCookie.split('=')[1];
        return JSON.parse(atob(encodedValue));
    }
    return {'user_id': '', 'name': ''};
}

class AJAX {
    constructor() {
        this.onSuccess = function(){};
        this.onError = function(){};
    }

    success(onSuccess) {
        this.onSuccess = onSuccess;
        return this;
    }

    error(onError) {
        this.onError = onError;
        return this;
    }

    get(url) {
        const xhr = new XMLHttpRequest();
        const onSuccess = this.onSuccess;
        const onError = this.onError;
        xhr.open('GET', url, true);
        // Handle network-level errors
        xhr.onerror = function () {
            console.error('XHR network error: unable to complete the request');
            onError(0, 'network error');
        };
        xhr.onreadystatechange = function () {
            if (xhr.readyState != 4) {
                return;
            }
            if (xhr.status === 0) {
                return;
            }
            if (xhr.status === 200) {
                const contentType = xhr.getResponseHeader('Content-Type');
                if (contentType && contentType.includes('application/json')) {
                    let resp = null;
                    try {
                        resp = JSON.parse(xhr.responseText)
                    } catch (e) {
                        console.error(`XHR JSON.parse error: unable to parse JSON: ${e.message}`);
                        onError(-1, e.message);
                        return;
                    }
                    onSuccess(resp);
                    return;
                }
                onSuccess(xhr.responseText);
                return;
            }
            // handle HTTP error
            console.error(`XHR HTTP error: ${xhr.status}: ${xhr.statusText} ${url}`);
            onError(xhr.status, xhr.responseText)
        };
        xhr.send();
    }

    postJSON(url, obj) {
        const xhr = new XMLHttpRequest();
        const onSuccess = this.onSuccess;
        const onError = this.onError;
        xhr.open('POST', url, true);
        xhr.setRequestHeader('Content-Type', 'application/json');
        // Handle network-level errors
        xhr.onerror = function () {
            console.error('XHR network error: unable to complete the request');
            onError(0, 'network error');
        };
        xhr.onreadystatechange = function () {
            if (xhr.readyState != 4) {
                return;
            }
            if (xhr.status === 0) {
                return;
            }
            if (xhr.status === 200) {
                const contentType = xhr.getResponseHeader('Content-Type');
                if (contentType && contentType.includes('application/json')) {
                    try {
                        onSuccess(JSON.parse(xhr.responseText));
                    } catch (e) {
                        console.error(`XHR JSON.parse error: unable to parse JSON: ${e.message}`);
                        onError(-1, e.message);
                    }
                    return;
                }
                onSuccess(xhr.responseText);
                return;
            }
            // handle HTTP error
            console.error(`XHR HTTP error: ${xhr.status}: ${xhr.statusText}`);
            onError(xhr.status, xhr.responseText)
        };
        // debug: console.info(`POST ${url} data:`, obj);
        xhr.send(JSON.stringify(obj));
    }
}

function ajax() {
    return new AJAX();
}

function generateChips() {
    const chips = [];
    const types = [{val: 5, color:'red'}, {val: 10, color: 'blue'}];
    for (let t of types) {
        for (let i = 0; i < 10; i++) {
            chips.push({val:t.val, color: t.color});
        }
    }
    return chips;
}

function generateCards() {
    const deck = [];
    for (let suit of suits) {
        for (let rank of ranks) {
            deck.push({ rank: rank, suit: suit, side: COVER });
        }
    }
    return deck.sort(() => 0.5 - Math.random());
}

function onItemMouseDown(e, item) {
    if (e.button != BUTTON_LEFT) {
        return;
    }
    let initialMouseX = e.clientX;
    let initialMouseY = e.clientY;

    let initialItemX = parseInt(item.style.left);
    let initialItemY = parseInt(item.style.top);

    function onMouseMove(event) {
        const deltaX = event.clientX - initialMouseX;
        const deltaY = event.clientY - initialMouseY;

        item.style.left = `${initialItemX + deltaX}px`;
        item.style.top = `${initialItemY + deltaY}px`;

        item.info.x = parseInt(item.style.left);
        item.info.y = parseInt(item.style.top);

        ajax().postJSON(window.location.pathname + '/update', item.info);
    }

    document.addEventListener('mousemove', onMouseMove);

    document.addEventListener('mouseup', () => {
        console.info('DEBUG mouse up: ', item.info);
        ajax().postJSON(window.location.pathname + '/update', item.info);
        document.removeEventListener('mousemove', onMouseMove);
    }, { once: true });
}

function onCardDblClick(e, card) {
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (card.info.owner_id != '' && card.info.owner_id != getSession().user_id) {
        return; // can't turn other player cards cards
    }
    card.info.side = card.info.side == COVER ? FACE: COVER;
    ajax().success((resp) => {
        if (resp.updated == null) {
            return;
        }
        card.info = resp.updated;
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/update`, card.info)
}

function newItem(cls, info, x, y) {
    const item = document.createElement('div');
    item.classList.add(cls);

    item.id = `item-${info.id}`
    item.info = info;
    item.style.top = `${y}px`;
    item.style.left = `${x}px`;

    item.ondragstart = () => false;
    // Make the item draggable
    item.addEventListener('mousedown', (e) => { onItemMouseDown(e, item); });

    item.render = () => {};

    return item;
}

function renderCard(card) {
    let text = '';
    let color = 'black';
    let side = card.info.side;
    let css = `card-${side}`

    card.classList.remove('card-cover', 'card-face', 'owned');
    card.style.borderColor = '';
    if (card.info.owner_id != '') {
        card.classList.add('owned');
        card.style.borderColor = players[card.info.owner_id].color || 'purple';
        // console.info(players[card.info.owner_id]);
    }
    if (side == FACE) {
        text = `${card.info.rank} ${card.info.suit}`;
        color = card.info.suit == '♥' || card.info.suit == '♦' ? 'red': 'black';
    }
    card.innerText = text;
    card.classList.add(css);
    card.style.color = color;
}

function takeCard(card) {
    if (card.info.owner_id != '') {
        return; // already owned
    }
    ajax().success((resp) => {
        console.info('take card result: ', resp);
        if (resp.updated != null) {
            updateItem(resp.updated);
        }
    }).postJSON(`${window.location.pathname}/take_card`,
        {'id': card.info.id});
}

function showCard(card) {
    if (card.info.owner_id != getSession().user_id) {
        return; // can't show not owned cards
    }
    ajax().success((resp) => {
        console.info('show card result: ', resp);
        if (resp.updated != null) {
            updateItem(resp.updated);
        }
    }).postJSON(`${window.location.pathname}/show_card`,
        {'id': card.info.id});
}

function newCard(info, x, y) {
    const card = newItem('card', info, x, y);
    card.addEventListener('click', (e) => {
        console.log('DEBUG', isKeyTPressed, isKeyOPressed, e);
        if (e.button != BUTTON_LEFT) {
            return;
        }
        if (e.ctrlKey || isKeyTPressed || e.metaKey) {
            takeCard(card);
        }
        if (e.shiftKey || isKeyOPressed) {
            showCard(card);
        }
    });
    card.addEventListener('dblclick', (e) => { onCardDblClick(e, card) });
    card.render = () => {  renderCard(card); };
    card.render();
    return card;
}

function newChip(info, x, y) {
    const chip = newItem('chip', info, x, y);
    chip.classList.add(`chip-${info.color}`);
    chip.innerText = info.val;
    return chip;
}

function newDealer(info, x, y) {
    const item = newItem('dealer', info, x, y);
    item.innerText = 'Dealer';
    return item;
}

function newPlayer(info, x, y) {
    const item = newItem('player', info, x, y);
    // HACK: gets data from global state due to .color property conflict
    const player = players[info.owner_id];
    item.classList.add(player.skin);
    item.innerText = player.Name;

    item.render = () => {
        // HACK
        item.style.left = ''; // use from css
        item.style.top = ''; // use from css
    };
    item.render();
    return item;
}

function updateItem(src) {
    if (src.id === null || src.id === undefined) {
        console.log('warn bad id', src);
        return;
    }
    let item = document.getElementById(`item-${src.id}`);
    if (item == null) {
        item = createItem(src);
    }
    item.info = src;
    item.style.top = `${src.y}px`;
    item.style.left = `${src.x}px`;
    item.render();
}

function updateTable(resp) {
    players = resp.Players;
    for (let it of resp.Items) {
        updateItem(it);
    }
}

function refresh(items) {
    ajax().success((resp) => {
        updateTable(resp)
    }).error((status, msg) => {
        if (status === 401) {
            window.location = '/';
        }
        console.error('refersh', status, msg);
    }).get(`${window.location.pathname}/state`);
}

function mkTableLocally(deck, chips) {
    let startX = 10;
    let startY = 170;
    // Add cards to the table
    const table = document.getElementById('card-table');
    deck.forEach((info) => {
        table.appendChild(newCard(info, startX, startY));
        startX += 1;
    });
    if (chips.length <1) {
        return;
    }
    let prev = chips[0];
    startX = 10;
    startY = 50;
    chips.forEach((info) => {
        if (prev.color != info.color) {
            startX += 100;
        }
        table.appendChild(newChip(info, startX, startY));

        startX += 5;
        prev = info;
    });
}

function createItem(info) {
    const table = document.getElementById('card-table');
    let item = null;
    switch (info.class) {
    case 'card':
        item = newCard(info, info.x, info.y);
        break;
    case 'chip':
        item = newChip(info, info.x, info.y);
        break;
    case 'dealer':
        item = newDealer(info, info.x, info.y);
        break;
    case 'player':
        item = newPlayer(info, info.x, info.y);
        break;
    default:
        throw new Exception(`unknown item class: ${info.class}`)
    }
    table.appendChild(item);
    return item;
}

let isKeyTPressed = false;
let isKeyOPressed = false;

function isKeyPressed(e, key) {
    try {
        return e.key.toLowerCase() === key;
    } catch {
        return false;
    }
}

function onLoad() {
    document.addEventListener('keydown', (event) => {
        if (isKeyPressed(event, 't')) {
            isKeyTPressed = true;
        }
        if (isKeyPressed(event, 'o')) {
            isKeyOPressed = true;
        }
    });
    document.addEventListener('keyup', (event) => {
        if (isKeyPressed(event, 't')) {
            isKeyTPressed = false;
        }
        if (isKeyPressed(event, 'o')) {
            isKeyOPressed = true;
        }
    });

    ajax().success((resp) => {
        console.info('initial table fetch:', resp);
        updateTable(resp);
        // setInterval(() => {
        //     refresh();
        // }, 10000);

        const socket = new WebSocket(`ws://${window.location.host}${window.location.pathname}/listen`);
        socket.onopen = () => {
            console.log('websocket connected');
            let banner = document.getElementById('offline-banner');
            banner.style.display = 'none';
        };
        socket.onclose = () => {
            console.log('websocket disconnected');
            let banner = document.getElementById('offline-banner');
            banner.style.display = 'block';
        };
        socket.onerror = (err) => { console.error('websocket error:', err); };
        socket.onmessage = (event) => {
            // console.log('websocket message:', typeof event.data);
            try {
                resp = JSON.parse(event.data)
                updateTable(resp);
            } catch (e) {
                // non-JSON payload
                if (event.data === 'refresh') {
                    location.reload();
                    return;
                }
                console.log(event.data);
            }
        };
    }).get(`${window.location.pathname}/state`);
}
